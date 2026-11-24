package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyVerifiesChecksumAndRollsBack(t *testing.T) {
	body := []byte("#!/bin/sh\necho agent\n")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = w.Write([]byte(hash + "  beszel-agent_linux_amd64\n"))
		default:
			_, _ = w.Write(body)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	bin := filepath.Join(dir, "beszel-agent")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := "https://github.com/Pradeeps-repo/beszel/releases/download/agent-latest"
	cfg := Config{
		Version:     "0.21.2-bw.4+abc",
		ReleaseBase: base,
		Repo:        "Pradeeps-repo/beszel",
		GOOS:        "linux",
		GOARCH:      "amd64",
		BinPath:     bin,
		Client:      srv.Client(),
		VersionOf:   func(string) (string, error) { return "0.21.2-bw.4+abc", nil },
	}
	// Point the client at the test server by rewriting the base check... the
	// URL must stay on github.com, so download through a transport instead.
	cfg.Client = srv.Client()
	cfg.Client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		clone.Host = clone.URL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
	restarted := false
	cfg.Restart = func() error {
		restarted = true
		return os.ErrPermission
	}
	_, err := Apply(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "backup restored") {
		t.Fatalf("restart failure should restore the backup, got %v", err)
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "old" {
		t.Fatalf("binary = %q", got)
	}
	if !restarted {
		t.Fatal("restart was not attempted")
	}

	cfg.Restart = func() error { return nil }
	if _, err := Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(bin)
	if string(got) != string(body) {
		t.Fatalf("installed %q", got)
	}
	backup, _ := os.ReadFile(bin + ".bak")
	if string(backup) != "old" {
		t.Fatalf("backup = %q", backup)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"))
			return
		}
		_, _ = w.Write([]byte("not-the-file"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	bin := filepath.Join(dir, "beszel-agent")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := srv.Client()
	client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		clone.Host = clone.URL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
	_, err := Apply(context.Background(), Config{
		Version: "0.21.2-bw.1+abc", ReleaseBase: "https://github.com/Pradeeps-repo/beszel/releases/download/agent-latest",
		Repo: "Pradeeps-repo/beszel", GOOS: "linux", GOARCH: "amd64", BinPath: bin, Client: client,
		VersionOf: func(string) (string, error) { return "0.21.2-bw.1+abc", nil },
	})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "old" {
		t.Fatalf("binary changed to %q", got)
	}
}

func TestHelpersAreIdempotent(t *testing.T) {
	root := t.TempDir()
	var calls []string
	opts := HelperOptions{
		DdcutilExists: true,
		Helper:        []byte("#!/bin/sh\necho display.json\n"),
		Runner: func(name string, args ...string) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		},
	}
	if err := InstallFleetHelpers(root, opts); err != nil {
		t.Fatal(err)
	}
	calls = nil
	if err := InstallFleetHelpers(root, opts); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if strings.Contains(call, "systemctl") || strings.Contains(call, "apt-get") {
			t.Fatalf("second run should not repeat %s", call)
		}
	}
	body, err := os.ReadFile(filepath.Join(root, "opt", "beszel-agent", "bright-watch-display.sh"))
	if err != nil || !strings.Contains(string(body), "display.json") {
		t.Fatal(err, string(body))
	}
}

func TestOutcome(t *testing.T) {
	if Outcome(true, "0.21.2-bw.2+abc", "0.21.2-bw.2+def") != "done" {
		t.Fatal("healthy matching build should stay")
	}
	if Outcome(false, "0.21.2-bw.2+abc", "0.21.2-bw.2+abc") != "rolled_back" {
		t.Fatal("unhealthy build should roll back")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }
