//go:build testing

package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPendingPackages(t *testing.T) {
	hub, user, other, server, userSystem := setupHub(t)
	session := mcpClient(t, server.URL, user.Token)
	otherSession := mcpClient(t, server.URL, other.Token)

	system1 := hubFindSystem(t, hub, userSystem.Id)
	system1.Set("status", "up")
	system1.Set("info", map[string]any{"osr": "Ubuntu 24.04", "pu": []uint16{2, 1}})
	system1.Set("package_updates", map[string]any{
		"manager": "apt", "checkedAt": 1700000000, "securityKnown": true,
		"packages": []map[string]any{
			{"name": "openssl", "current": "3.0.2", "available": "3.0.13", "security": true},
			{"name": "bash", "current": "5.1", "available": "5.2"},
		},
	})
	require.NoError(t, hub.Save(system1))

	system2 := findSystemByName(t, hub, "user-system-2")
	system2.Set("status", "up")
	system2.Set("info", map[string]any{"osr": "Ubuntu 22.04"})
	system2.Set("package_updates", map[string]any{
		"packages": []map[string]any{
			{"name": "openssl", "current": "3.0.2", "available": "3.0.13", "security": true},
		},
	})
	require.NoError(t, hub.Save(system2))

	foreign := findSystemByName(t, hub, "other-system")
	foreign.Set("status", "up")
	foreign.Set("info", map[string]any{"osr": "Ubuntu 24.04"})
	foreign.Set("package_updates", map[string]any{
		"packages": []map[string]any{{"name": "secret-pkg", "available": "9"}},
	})
	require.NoError(t, hub.Save(foreign))

	detail := callTool(t, session, "get_system", map[string]any{"system": "user-system-1"})
	require.Equal(t, []any{
		map[string]any{"name": "openssl", "current": "3.0.2", "available": "3.0.13", "security": true},
		map[string]any{"name": "bash", "current": "5.1", "available": "5.2"},
	}, detail["packages"])
	require.Equal(t, map[string]any{"total": float64(2), "security": float64(1)}, detail["updates"])

	list := callTool(t, session, "list_pending_packages", nil)
	require.Equal(t, float64(2), list["total"])
	rows := list["packages"].([]any)
	require.Equal(t, map[string]any{
		"name": "openssl", "available": "3.0.13", "security": true, "system_count": float64(2),
	}, rows[0])
	require.Equal(t, map[string]any{
		"name": "bash", "available": "5.2", "system_count": float64(1),
	}, rows[1])

	security := callTool(t, session, "list_pending_packages", map[string]any{"security_only": true})
	require.Equal(t, float64(1), security["total"])
	require.Equal(t, "openssl", security["packages"].([]any)[0].(map[string]any)["name"])

	named := callTool(t, session, "list_pending_packages", map[string]any{"package": "BAS"})
	require.Equal(t, "bash", named["packages"].([]any)[0].(map[string]any)["name"])

	old := callTool(t, session, "list_pending_packages", map[string]any{"os": "22.04"})
	require.Equal(t, float64(1), old["total"])
	require.Equal(t, float64(1), old["packages"].([]any)[0].(map[string]any)["system_count"])

	down := callTool(t, session, "list_pending_packages", map[string]any{"status": "down"})
	require.Equal(t, float64(0), down["total"])
	require.Equal(t, []any{}, down["packages"])

	page := callTool(t, session, "list_pending_packages", map[string]any{"limit": 1})
	require.Equal(t, float64(1), page["count"])
	require.Equal(t, float64(2), page["total"])
	require.Equal(t, true, page["truncated"])
	require.Equal(t, "openssl", page["packages"].([]any)[0].(map[string]any)["name"])
	next := callTool(t, session, "list_pending_packages", map[string]any{"limit": 1, "offset": 1})
	require.Equal(t, "bash", next["packages"].([]any)[0].(map[string]any)["name"])
	require.NotContains(t, next, "truncated")

	filtered := callTool(t, session, "list_systems", map[string]any{"package": "openssl"})
	require.Equal(t, float64(2), filtered["count"])
	bashOnly := callTool(t, session, "list_systems", map[string]any{"package": "bash"})
	require.Equal(t, float64(1), bashOnly["count"])
	require.Equal(t, "user-system-1", bashOnly["systems"].([]any)[0].(map[string]any)["name"])
	require.NotContains(t, bashOnly["systems"].([]any)[0], "packages")

	hidden := callTool(t, otherSession, "list_pending_packages", nil)
	require.Equal(t, float64(1), hidden["total"])
	require.Equal(t, "secret-pkg", hidden["packages"].([]any)[0].(map[string]any)["name"])
	require.Contains(t, callToolError(t, session, "get_system", map[string]any{"system": "other-system"}), "not found or not visible")
}
