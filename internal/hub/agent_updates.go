package hub

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel/internal/agentupdate"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/henrygd/beszel/internal/hub/utils"
)

const agentUpdateTimeout = 3 * time.Minute

type agentReleaseCache struct {
	mu      sync.Mutex
	at      time.Time
	version string
	commit  string
}

type agentUpdateState struct {
	State  string `json:"state,omitempty"`
	Error  string `json:"error,omitempty"`
	Target string `json:"target,omitempty"`
	At     string `json:"at,omitempty"`
	Forced bool   `json:"forced,omitempty"`
}

type agentUpdateRequest struct {
	Force   bool     `json:"force"`
	All     bool     `json:"all"`
	Systems []string `json:"systems"`
	Filter  struct {
		Status string `json:"status"`
		Name   string `json:"name"`
		Behind bool   `json:"behind"`
	} `json:"filter"`
}

type agentUpdateProgress struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Error  string `json:"error,omitempty"`
	Target string `json:"target,omitempty"`
	Forced bool   `json:"forced,omitempty"`
}

func (h *Hub) cachedAgentRelease() (string, string) {
	h.agentRelease.mu.Lock()
	defer h.agentRelease.mu.Unlock()
	return h.agentRelease.version, h.agentRelease.commit
}

func (h *Hub) refreshAgentRelease(force bool) {
	h.agentRelease.mu.Lock()
	if !force && time.Since(h.agentRelease.at) < 10*time.Minute && h.agentRelease.version != "" {
		h.agentRelease.mu.Unlock()
		return
	}
	h.agentRelease.at = time.Now()
	h.agentRelease.mu.Unlock()
	repo, _ := utils.GetEnv("AGENT_RELEASE_REPO")
	tag, _ := utils.GetEnv("AGENT_RELEASE_TAG")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file, err := agentupdate.FetchVersion(ctx, http.DefaultClient, repo, tag)
	if err != nil {
		h.Logger().Debug("agent release lookup failed", "err", err)
		return
	}
	h.agentRelease.mu.Lock()
	h.agentRelease.version = file.Version
	h.agentRelease.commit = file.Commit
	h.agentRelease.at = time.Now()
	h.agentRelease.mu.Unlock()
}

func (h *Hub) agentUpdateSettings() (agentupdate.Settings, error) {
	return agentupdate.Load(h.DataDir(), utils.GetEnv)
}

func (h *Hub) effectiveAgentTarget(settings agentupdate.Settings) string {
	if pin := strings.TrimSpace(settings.Target); pin != "" {
		return pin
	}
	version, _ := h.cachedAgentRelease()
	return version
}

func (h *Hub) getAgentUpdates(e *core.RequestEvent) error {
	h.refreshAgentRelease(false)
	settings, err := h.agentUpdateSettings()
	if err != nil {
		return e.InternalServerError("Could not read agent update settings", err)
	}
	version, commit := h.cachedAgentRelease()
	progress, err := h.agentUpdateProgress()
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{
		"settings": settings,
		"latest":   map[string]string{"version": version, "commit": commit},
		"target":   h.effectiveAgentTarget(settings),
		"progress": progress,
	})
}

func (h *Hub) putAgentUpdateSettings(e *core.RequestEvent) error {
	var settings agentupdate.Settings
	if err := e.BindBody(&settings); err != nil {
		return e.BadRequestError("Bad data", err)
	}
	if err := agentupdate.Save(h.DataDir(), settings); err != nil {
		return e.InternalServerError("Could not save agent update settings", err)
	}
	saved, err := h.agentUpdateSettings()
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, saved)
}

func (h *Hub) postAgentUpdates(e *core.RequestEvent) error {
	var req agentUpdateRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("Bad data", err)
	}
	if !req.Force {
		return e.BadRequestError("Set force to queue systems outside the rollout", nil)
	}
	if !req.All && len(req.Systems) == 0 && req.Filter.Status == "" && req.Filter.Name == "" && !req.Filter.Behind {
		return e.BadRequestError("Choose all systems, a list, or a filter", nil)
	}
	result, err := h.queueAgentUpdates(req)
	if err != nil {
		return e.BadRequestError(err.Error(), nil)
	}
	go h.dispatchAgentUpdates(context.Background())
	return e.JSON(http.StatusOK, result)
}

type agentUpdateQueueResult struct {
	Queued  int                   `json:"queued"`
	Skipped int                   `json:"skipped"`
	Target  string                `json:"target"`
	Systems []agentUpdateProgress `json:"systems"`
}

func (h *Hub) queueAgentUpdates(req agentUpdateRequest) (agentUpdateQueueResult, error) {
	settings, err := h.agentUpdateSettings()
	if err != nil {
		return agentUpdateQueueResult{}, err
	}
	h.refreshAgentRelease(false)
	target := h.effectiveAgentTarget(settings)
	if target == "" {
		return agentUpdateQueueResult{}, errNoAgentTarget
	}
	records, err := h.allSystemRecords()
	if err != nil {
		return agentUpdateQueueResult{}, err
	}
	wanted := map[string]struct{}{}
	for _, id := range req.Systems {
		wanted[id] = struct{}{}
	}
	result := agentUpdateQueueResult{Target: target}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, record := range records {
		if !agentUpdateSelected(record, req, wanted, target) {
			continue
		}
		state := readAgentUpdate(record)
		if strings.EqualFold(record.GetString("status"), "up") == false {
			result.Skipped++
			continue
		}
		var info system.Info
		_ = record.UnmarshalJSONField("info", &info)
		if agentupdate.Compare(info.AgentVersion, target) >= 0 {
			result.Skipped++
			continue
		}
		if state.State == "pending" || state.State == "updating" {
			result.Queued++
			continue
		}
		next := agentUpdateState{State: "pending", Target: target, At: now, Forced: true}
		if err := h.writeAgentUpdate(record, next); err != nil {
			return agentUpdateQueueResult{}, err
		}
		result.Queued++
		result.Systems = append(result.Systems, agentUpdateProgress{
			ID: record.Id, Name: record.GetString("name"), State: next.State, Target: target, Forced: true,
		})
	}
	return result, nil
}

var errNoAgentTarget = agentTargetError{}

type agentTargetError struct{}

func (agentTargetError) Error() string {
	return "the latest agent version is not known yet"
}

func agentUpdateSelected(record *core.Record, req agentUpdateRequest, wanted map[string]struct{}, target string) bool {
	if req.All {
		return true
	}
	if _, ok := wanted[record.Id]; ok {
		return true
	}
	if _, ok := wanted[record.GetString("name")]; ok {
		return true
	}
	if req.Filter.Status == "" && req.Filter.Name == "" && !req.Filter.Behind {
		return false
	}
	if status := strings.TrimSpace(req.Filter.Status); status != "" && !strings.EqualFold(record.GetString("status"), status) {
		return false
	}
	if name := strings.TrimSpace(req.Filter.Name); name != "" && !strings.Contains(strings.ToLower(record.GetString("name")), strings.ToLower(name)) {
		return false
	}
	if req.Filter.Behind {
		var info system.Info
		_ = record.UnmarshalJSONField("info", &info)
		if agentupdate.Compare(info.AgentVersion, target) >= 0 {
			return false
		}
	}
	return req.Filter.Status != "" || req.Filter.Name != "" || req.Filter.Behind
}

func (h *Hub) dispatchAgentUpdates(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			h.Logger().Error("agent update dispatch stopped", "err", recovered)
		}
	}()
	settings, err := h.agentUpdateSettings()
	if err != nil {
		h.Logger().Error("agent update settings", "err", err)
		return
	}
	h.refreshAgentRelease(false)
	target := h.effectiveAgentTarget(settings)
	records, err := h.allSystemRecords()
	if err != nil {
		h.Logger().Error("agent update systems", "err", err)
		return
	}
	now := time.Now().UTC()
	var pending []agentupdate.System
	inFlight := 0
	for _, record := range records {
		state := readAgentUpdate(record)
		var info system.Info
		_ = record.UnmarshalJSONField("info", &info)
		if state.State == "updating" {
			inFlight++
			h.reconcileAgentUpdate(record, state, info.AgentVersion, now)
			if readAgentUpdate(record).State == "updating" {
				continue
			}
			inFlight--
		}
		if state.State == "pending" {
			pending = append(pending, agentupdate.System{
				ID: record.Id, Name: record.GetString("name"), Status: record.GetString("status"),
				Version: info.AgentVersion, State: state.State, Forced: state.Forced,
			})
			continue
		}
		if target == "" {
			continue
		}
		candidate := agentupdate.System{
			ID: record.Id, Name: record.GetString("name"), Status: record.GetString("status"),
			Version: info.AgentVersion, State: state.State,
		}
		if agentupdate.AutoEligible(candidate, settings, target, now, false) {
			next := agentUpdateState{State: "pending", Target: target, At: now.Format(time.RFC3339)}
			if err := h.writeAgentUpdate(record, next); err != nil {
				h.Logger().Error("queue agent update", "system", record.Id, "err", err)
				continue
			}
			pending = append(pending, candidate)
		}
	}
	slots := settings.Concurrency - inFlight
	for _, item := range agentupdate.Batch(pending, slots) {
		record, err := h.FindRecordById("systems", item.ID)
		if err != nil {
			continue
		}
		state := readAgentUpdate(record)
		state.State = "updating"
		state.At = now.Format(time.RFC3339)
		if state.Target == "" {
			state.Target = target
		}
		if err := h.writeAgentUpdate(record, state); err != nil {
			continue
		}
		go h.sendAgentUpdate(item.ID, state.Target)
	}
	_ = ctx
}

func (h *Hub) sendAgentUpdate(id, target string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			h.Logger().Error("agent update send stopped", "system", id, "err", recovered)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), agentUpdateTimeout)
	defer cancel()
	sys, err := h.sm.GetSystem(id)
	if err != nil {
		h.markAgentUpdate(id, "failed", "system is not connected")
		return
	}
	_, commit := h.cachedAgentRelease()
	response, err := sys.RequestAgentUpdate(ctx, target, commit)
	if err != nil {
		message := err.Error()
		if strings.Contains(strings.ToLower(message), "unknown action") {
			message = "this agent does not accept hub updates yet; run upgrade-agent.sh once"
		}
		h.markAgentUpdate(id, "failed", message)
		return
	}
	if !response.Accepted {
		state := "failed"
		if response.Status == "current" {
			state = "done"
		}
		h.markAgentUpdate(id, state, response.Error)
		return
	}
	if response.Error != "" {
		h.markAgentUpdate(id, "updating", response.Error)
	}
}

func (h *Hub) reconcileAgentUpdate(record *core.Record, state agentUpdateState, version string, now time.Time) {
	if state.Target != "" && agentupdate.Compare(version, state.Target) == 0 {
		state.State = "done"
		state.Error = ""
		_ = h.writeAgentUpdate(record, state)
		return
	}
	at, err := time.Parse(time.RFC3339, state.At)
	if err == nil && now.Sub(at) > 10*time.Minute {
		state.State = "rolled_back"
		if state.Error == "" {
			state.Error = "the new version did not stay healthy"
		}
		_ = h.writeAgentUpdate(record, state)
	}
}

func (h *Hub) markAgentUpdate(id, state, message string) {
	record, err := h.FindRecordById("systems", id)
	if err != nil {
		return
	}
	current := readAgentUpdate(record)
	current.State = state
	current.Error = message
	if current.At == "" {
		current.At = time.Now().UTC().Format(time.RFC3339)
	}
	_ = h.writeAgentUpdate(record, current)
}

func (h *Hub) agentUpdateProgress() ([]agentUpdateProgress, error) {
	records, err := h.allSystemRecords()
	if err != nil {
		return nil, err
	}
	out := []agentUpdateProgress{}
	for _, record := range records {
		state := readAgentUpdate(record)
		if state.State == "" || state.State == "idle" {
			continue
		}
		out = append(out, agentUpdateProgress{
			ID: record.Id, Name: record.GetString("name"), State: state.State,
			Error: state.Error, Target: state.Target, Forced: state.Forced,
		})
	}
	return out, nil
}

func (h *Hub) allSystemRecords() ([]*core.Record, error) {
	var all []*core.Record
	for offset := 0; ; offset += 500 {
		records, err := h.FindRecordsByFilter("systems", "", "name", 500, offset, nil)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
		if len(records) < 500 {
			return all, nil
		}
	}
}

func readAgentUpdate(record *core.Record) agentUpdateState {
	var state agentUpdateState
	if record == nil || record.Get("agent_update") == nil {
		return state
	}
	_ = record.UnmarshalJSONField("agent_update", &state)
	return state
}

func (h *Hub) writeAgentUpdate(record *core.Record, state agentUpdateState) error {
	record.Set("agent_update", state)
	return h.SaveNoValidate(record)
}
