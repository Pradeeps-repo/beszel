//go:build testing

package hub_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/require"
)

func TestAgentUpdateAPI(t *testing.T) {
	hub, handler := firstUserTestMux(t)
	t.Cleanup(hub.Cleanup)
	admin, err := beszelTests.CreateUserWithRole(hub, "admin-update@example.com", "password", "admin")
	require.NoError(t, err)
	adminToken, err := admin.NewAuthToken()
	require.NoError(t, err)
	user, err := beszelTests.CreateUser(hub, "user-update@example.com", "password")
	require.NoError(t, err)
	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	denied := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/beszel/agent-updates", strings.NewReader(`{"force":true,"all":true}`))
	req.Header.Set("Authorization", userToken)
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(denied, req)
	require.Equal(t, http.StatusForbidden, denied.Code)

	save := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/beszel/agent-updates/settings", strings.NewReader(`{"enabled":false,"target":"0.21.2-bw.8+abc","percent":10,"window_start":"01:00","window_end":"05:00","concurrency":5}`))
	req.Header.Set("Authorization", adminToken)
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(save, req)
	require.Equal(t, http.StatusOK, save.Code, save.Body.String())
	require.Contains(t, save.Body.String(), `"enabled":false`)

	record, err := beszelTests.CreateRecord(hub, "systems", map[string]any{
		"name": "kiosk-a", "host": "127.0.0.1", "users": []string{admin.Id},
	})
	require.NoError(t, err)
	record.Set("status", "up")
	record.Set("info", map[string]any{"v": "0.21.2"})
	require.NoError(t, hub.Save(record))

	queued := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/beszel/agent-updates", strings.NewReader(`{"force":true,"all":true}`))
	req.Header.Set("Authorization", adminToken)
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(queued, req)
	require.Equal(t, http.StatusOK, queued.Code, queued.Body.String())
	require.Contains(t, queued.Body.String(), `"queued":1`)
	require.Contains(t, queued.Body.String(), `"forced":true`)
}
