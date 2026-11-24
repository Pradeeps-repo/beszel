//go:build testing

package mcp_test

import (
	"testing"

	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/require"
)

func TestFleetListSystemsFilters(t *testing.T) {
	hub, user, _, server, userSystem := setupHub(t)
	session := mcpClient(t, server.URL, user.Token)
	system := hubFindSystem(t, hub, userSystem.Id)
	system.Set("info", map[string]any{
		"cpu": 10, "mp": 20, "dp": 30,
		"br": 40, "res": "1080x1920", "mod": "1920x1080", "ori": "portrait (90°)",
		"osr": "Ubuntu 24.04", "k": "6.8.7", "ssa": "1.4.0", "ssi": "2.0.0",
	})
	require.NoError(t, hub.Save(system))

	data := callTool(t, session, "list_systems", map[string]any{
		"status": "up", "name": "system-1", "orientation": "portrait", "resolution": "1080x1920",
		"ss_app":         map[string]any{"gt": "1.2.0", "lt": "2.0.0"},
		"ss_iot":         map[string]any{"eq": "2.0.0"},
		"brightness_min": 25, "brightness_max": 50,
		"limit": 10,
	})
	require.Equal(t, float64(1), data["count"])
	row := data["systems"].([]any)[0].(map[string]any)
	require.Equal(t, "user-system-1", row["name"])
	require.Equal(t, float64(40), row["brightness_pct"])
	require.Equal(t, "1080x1920", row["resolution"])
	require.Equal(t, "1920x1080", row["mode"])
	require.Equal(t, "portrait (90°)", row["orientation"])
	require.Equal(t, "Ubuntu 24.04", row["os"])
	require.Equal(t, "6.8.7", row["kernel"])
	require.Equal(t, "1.4.0", row["ss_app"])
	require.Equal(t, "2.0.0", row["ss_iot"])
	require.Equal(t, float64(10), row["cpu_pct"])

	none := callTool(t, session, "list_systems", map[string]any{"ss_app": map[string]any{"eq": "9.9.9"}})
	require.Equal(t, float64(0), none["count"])
}

func TestFleetGetSystemAndHistory(t *testing.T) {
	hub, user, _, server, userSystem := setupHub(t)
	session := mcpClient(t, server.URL, user.Token)
	br := uint8(55)
	createStatsRecord(t, hub, userSystem.Id, map[string]any{
		"cpu": 12, "mp": 34, "dp": 56, "t": map[string]float64{"cpu": 47}, "b": [2]uint64{100, 200}, "br": br,
		"g": map[string]any{"0": map[string]any{"n": "GPU", "u": 15}},
	}, 5)
	detail := callTool(t, session, "get_system", map[string]any{"system": userSystem.Id})
	require.Equal(t, "user-system-1", detail["name"])
	current := detail["current"].(map[string]any)
	require.Equal(t, float64(47), current["temperatures"].(map[string]any)["cpu"])
	require.Equal(t, float64(15), current["gpus"].(map[string]any)["0"].(map[string]any)["u"])

	history := callTool(t, session, "get_system_history", map[string]any{
		"system": "user-system-1", "range": "1h", "metrics": []string{"cpu", "brightness", "network"},
	})
	require.Equal(t, "1m", history["period"])
	samples := history["samples"].([]any)
	require.NotEmpty(t, samples)
	sample := samples[0].(map[string]any)
	require.Equal(t, float64(12), sample["cpu"])
	require.Equal(t, float64(55), sample["brightness"])
	require.Equal(t, float64(100), sample["net_sent_bps"])
	require.NotContains(t, sample, "mem")

	errText := callToolError(t, session, "get_system_history", map[string]any{"system": "user-system-1", "range": "2h", "metrics": []string{"cpu"}})
	require.Contains(t, errText, "range")
}

func TestFleetSummaryAndAlerts(t *testing.T) {
	hub, user, _, server, userSystem := setupHub(t)
	session := mcpClient(t, server.URL, user.Token)
	system := hubFindSystem(t, hub, userSystem.Id)
	system.Set("info", map[string]any{
		"br": 80, "res": "1080x1920", "ori": "portrait", "osr": "Ubuntu 24.04", "ssa": "1.4.0", "ssi": "2.0.0",
	})
	require.NoError(t, hub.Save(system))
	_, err := beszelTests.CreateRecord(hub, "alerts", map[string]any{
		"name": "CPU", "user": user.Record.Id, "system": userSystem.Id, "value": 90, "triggered": true,
	})
	require.NoError(t, err)
	_, err = beszelTests.CreateRecord(hub, "alerts_history", map[string]any{
		"user": user.Record.Id, "system": userSystem.Id, "alert_id": "alert-1", "name": "CPU", "value": 91,
	})
	require.NoError(t, err)

	summary := callTool(t, session, "fleet_summary", map[string]any{"stale_minutes": 60})
	require.Equal(t, float64(2), summary["total"])
	require.Equal(t, float64(1), summary["by_ss_app"].(map[string]any)["1.4.0"])
	require.Equal(t, float64(1), summary["by_orientation"].(map[string]any)["portrait"])
	require.Equal(t, float64(1), summary["by_brightness"].(map[string]any)["75-100"])
	require.Equal(t, float64(1), summary["by_os"].(map[string]any)["Ubuntu 24.04"])

	alerts := callTool(t, session, "list_alerts", nil)
	require.Len(t, alerts["alerts"].([]any), 1)
	require.Len(t, alerts["history"].([]any), 1)
	require.Equal(t, "CPU", alerts["history"].([]any)[0].(map[string]any)["name"])
}

func TestMcpAgentVersionFilterAndUpdateStatus(t *testing.T) {
	hub, user, _, server, userSystem := setupHub(t)
	session := mcpClient(t, server.URL, user.Token)
	system := hubFindSystem(t, hub, userSystem.Id)
	system.Set("info", map[string]any{"v": "0.21.2-bw.4+abc", "bc": "abc"})
	system.Set("agent_update", map[string]any{"state": "failed", "error": "sha256 mismatch"})
	require.NoError(t, hub.Save(system))
	other := findSystemByName(t, hub, "user-system-2")
	other.Set("info", map[string]any{"v": "0.21.2"})
	require.NoError(t, hub.Save(other))

	data := callTool(t, session, "list_systems", map[string]any{
		"agent_version": map[string]any{"gt": "0.21.2", "lt": "0.21.2-bw.10"},
	})
	require.Equal(t, float64(1), data["count"])
	row := data["systems"].([]any)[0].(map[string]any)
	require.Equal(t, "0.21.2-bw.4+abc", row["agent_version"])
	require.Equal(t, "abc", row["agent_commit"])
	require.Equal(t, "failed", row["agent_update"])
	require.Equal(t, "sha256 mismatch", row["agent_update_error"])

	detail := callTool(t, session, "get_system", map[string]any{"system": "user-system-1"})
	require.Equal(t, "failed", detail["agent_update"])
}

func TestMcpTokenAuth(t *testing.T) {
	t.Setenv("BESZEL_HUB_MCP", "")
	t.Setenv("MCP", "")
	t.Setenv("MCP_TOKEN", "fleet-secret")
	hub, err := beszelTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(hub.Cleanup)
	hub.StartHub()
	server := mcpServer(t, hub)
	user := newTestUser(t, hub, "token-user@example.com")
	other := newTestUser(t, hub, "token-other@example.com")
	createSystem(t, hub, "owned", user.Record.Id)
	createSystem(t, hub, "foreign", other.Record.Id)

	session := mcpClient(t, server.URL, "Bearer fleet-secret")
	data := callTool(t, session, "list_systems", nil)
	require.Equal(t, float64(2), data["total"], "the shared token can see every system")

	userSession := mcpClient(t, server.URL, user.Token)
	scoped := callTool(t, userSession, "list_systems", nil)
	require.Equal(t, float64(1), scoped["total"])
}
