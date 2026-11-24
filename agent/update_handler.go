package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel"
	"github.com/henrygd/beszel/agent/selfupdate"
	"github.com/henrygd/beszel/internal/agentupdate"
	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/ghupdate"
)

// ApplyAgentUpdateHandler installs the release the hub named.
// The download URL is the agent's configured release, never a hub-supplied command.
type ApplyAgentUpdateHandler struct{}

func (h *ApplyAgentUpdateHandler) Handle(hctx *HandlerContext) error {
	if ghupdate.SelfUpdateDisabled {
		return hctx.SendResponse(common.AgentUpdateResponse{Status: "failed", Error: ghupdate.ErrSelfUpdateDisabled.Error()}, hctx.RequestID)
	}
	var req common.AgentUpdateRequest
	if err := cbor.Unmarshal(hctx.Request.Data, &req); err != nil {
		return err
	}
	req.Version = strings.TrimSpace(req.Version)
	if req.Version == "" {
		return hctx.SendResponse(common.AgentUpdateResponse{Status: "failed", Error: "target version is required"}, hctx.RequestID)
	}
	if agentupdate.Compare(beszel.Version, req.Version) == 0 {
		return hctx.SendResponse(common.AgentUpdateResponse{
			Accepted: false,
			Status:   "current",
			Version:  beszel.Version,
			Commit:   beszel.Commit,
		}, hctx.RequestID)
	}
	repo := strings.TrimSpace(os.Getenv("BESZEL_AGENT_RELEASE_REPO"))
	if repo == "" {
		repo = agentupdate.DefaultRepo
	}
	base := strings.TrimSpace(os.Getenv("BESZEL_AGENT_RELEASE_BASE"))
	if base == "" {
		base = agentupdate.ReleaseBase(repo, agentupdate.DefaultTag)
	}
	bin, err := os.Executable()
	if err != nil {
		return hctx.SendResponse(common.AgentUpdateResponse{Status: "failed", Error: err.Error()}, hctx.RequestID)
	}
	var helperErr string
	if !runningAsRoot() {
		if err := exec.Command("sudo", "-n", bin, "install-fleet-helpers").Run(); err != nil {
			helperErr = "fleet helpers were not installed: " + err.Error()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, applyErr := selfupdate.Apply(ctx, selfupdate.Config{
		Version:     req.Version,
		ReleaseBase: base,
		Repo:        repo,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		BinPath:     bin,
		IsRoot:      runningAsRoot(),
		InstallHelp: func() error {
			return selfupdate.InstallFleetHelpers("", selfupdate.HelperOptions{
				DdcutilExists: commandExists("ddcutil"),
			})
		},
		ArmWatcher: func(expect string) error {
			return exec.Command(bin, "update-finish", "--expect", expect, "--bin", bin).Start()
		},
	})
	if applyErr != nil {
		slog.Error("agent update failed", "err", applyErr)
		return hctx.SendResponse(common.AgentUpdateResponse{Status: "failed", Error: applyErr.Error()}, hctx.RequestID)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := selfupdateRestart(); err != nil {
			slog.Error("agent update restart failed", "err", err)
		}
	}()
	return hctx.SendResponse(common.AgentUpdateResponse{
		Accepted: true,
		Status:   "accepted",
		Error:    helperErr,
		Version:  beszel.Version,
		Commit:   beszel.Commit,
	}, hctx.RequestID)
}

func selfupdateRestart() error {
	return exec.Command("systemctl", "restart", "beszel-agent.service").Run()
}
