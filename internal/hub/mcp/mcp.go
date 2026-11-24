// Package mcp exposes the hub's monitoring data to AI agents over the Model
// Context Protocol (MCP).
//
// The server is mounted on the /mcp route (streamable HTTP transport) when
// MCP_TOKEN is set, or when BESZEL_HUB_MCP=true. MCP_TOKEN is a bearer secret
// that can read every system. PocketBase user, superuser and API-key tokens
// are also accepted and only see that account's systems. Only read-only tools
// are exposed.
package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel"
	"github.com/henrygd/beszel/internal/hub/utils"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterRoutes registers the /mcp route when MCP is enabled.
// Set MCP_TOKEN (or BESZEL_HUB_MCP_TOKEN) to a bearer secret, or set
// BESZEL_HUB_MCP=true to accept PocketBase user and superuser tokens.
// The route is not registered when both are unset.
func RegisterRoutes(app core.App, se *core.ServeEvent) error {
	_, tokenSet := configuredMCPToken()
	enabled, _ := utils.GetEnv("MCP")
	if !tokenSet && enabled != "true" {
		return nil
	}
	server := newServer(app)
	handler := mcpsdk.NewStreamableHTTPHandler(func(_ *http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{
		// all tools are read-only and authorize every request, so no session
		// state is needed (stateful sessions are never freed if clients don't
		// send DELETE)
		Stateless: true,
		// requests are authenticated with a token header rather than cookies,
		// so DNS rebinding is not a concern, and the protection rejects hubs
		// behind a reverse proxy on the same host (loopback address with a
		// public Host header)
		DisableLocalhostProtection: true,
	})
	// the auth token is resolved by PocketBase and requireAuth answers 401 for
	// missing/invalid tokens, so MCP clients only need to pass an API key in
	// the Authorization header, e.g. Authorization: Bearer <key>
	// (the stateless transport uses POST for messages and answers GET and
	// DELETE with 405 so clients know there is no SSE stream or session; each
	// method is registered separately as a method wildcard route would
	// conflict with the static file catch-all route, which would otherwise
	// serve the web UI for GET /mcp)
	mcpHandler := func(e *core.RequestEvent) error {
		// pass the auth record resolved by PocketBase to the tool handlers
		// (stateless sessions run tool calls with the request context)
		ctx := context.WithValue(e.Request.Context(), authContextKey{}, e.Auth)
		handler.ServeHTTP(e.Response, e.Request.WithContext(ctx))
		return nil
	}
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		se.Router.Route(method, "/mcp", mcpHandler).BindFunc(requireAuth)
	}
	return nil
}

// requireAuth rejects requests without a valid auth record, like
// apis.RequireAuth, but explains where to get an API key since MCP clients
// are usually configured by hand.
func requireAuth(e *core.RequestEvent) error {
	if token, ok := configuredMCPToken(); ok && bearerMatches(e.Request, token) {
		record, err := mcpServiceRecord(e.App)
		if err != nil {
			return e.InternalServerError("MCP token could not be applied", err)
		}
		e.Auth = record
		return e.Next()
	}
	if e.Auth != nil {
		return e.Next()
	}
	settingsPage := "Settings > API Keys"
	if appURL := strings.TrimSuffix(e.App.Settings().Meta.AppURL, "/"); appURL != "" {
		settingsPage += " (" + appURL + "/settings/api-keys)"
	}
	// PocketBase ignores invalid tokens, so a present header means the key
	// is invalid, expired or revoked. Per RFC 6750, the error code is only
	// included when credentials were sent. There is no resource_metadata
	// parameter, as OAuth is not supported.
	const usage = " and send it in the Authorization header as \"Bearer <key>\"."
	msg := "Missing API key. Create one in " + settingsPage + usage
	challenge := `Bearer realm="beszel"`
	if e.Request.Header.Get("Authorization") != "" {
		msg = "Invalid, expired or revoked API key. Create a new one in " + settingsPage + usage
		challenge += `, error="invalid_token", error_description="` + strings.ReplaceAll(msg, `"`, `'`) + `"`
	}
	e.Response.Header().Set("WWW-Authenticate", challenge)
	return e.UnauthorizedError(msg, nil)
}

// configuredMCPToken is the optional hub-wide bearer secret. An empty value
// disables this auth method. PocketBase user tokens still work when the
// route is enabled with BESZEL_HUB_MCP=true.
func configuredMCPToken() (string, bool) {
	value, ok := utils.GetEnv("MCP_TOKEN")
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

func bearerMatches(r *http.Request, token string) bool {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(header[len(prefix):])
	if len(got) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// mcpServiceRecord is an unsaved superuser so the shared token can read every
// system. It is never written to the database.
func mcpServiceRecord(app core.App) (*core.Record, error) {
	collection, err := app.FindCachedCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(collection)
	record.Id = "mcp"
	return record, nil
}

// newServer creates an MCP server with the hub's read-only tools.
func newServer(app core.App) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "beszel",
		Version: beszel.Version,
	}, &mcpsdk.ServerOptions{
		Instructions: "Beszel is a lightweight server monitoring hub. " +
			"Use list_systems to filter systems (status, name, package, ss-app, ss-iot, orientation, resolution, brightness). " +
			"get_system returns one system including sensors, GPUs and pending packages. " +
			"list_pending_packages aggregates pending packages across the visible fleet. " +
			"get_system_history returns cpu, memory, disk, temperature, network and brightness samples for 1h, 12h, 24h, 1w or 30d. " +
			"fleet_summary counts the visible fleet. list_alerts returns alert rules and recent history. " +
			"Data access matches the authenticated user, except the MCP_TOKEN bearer which can read the whole fleet.",
	})
	registerTools(app, server)
	return server
}

// mcphub bundles an app with the MCP server so tool handlers can query data.
type mcphub struct {
	app core.App
}

// notVisibleMsg is shared for missing and inaccessible systems so callers
// cannot distinguish between the two.
const notVisibleMsg = "not found or not visible to this user"

// authContextKey is the request context key of the auth record.
type authContextKey struct{}

// authRecord returns the authenticated record (regular user or superuser)
// that PocketBase resolved from the Authorization header of the HTTP request
// carrying the tool call. Access is enforced by the auth middleware on the
// /mcp route, but tools use the record to scope queries to visible systems.
func (h *mcphub) authRecord(ctx context.Context) (*core.Record, error) {
	record, _ := ctx.Value(authContextKey{}).(*core.Record)
	if record == nil {
		return nil, errors.New("missing authenticated user; create an API key in Settings > API Keys and send it in the Authorization header of your MCP client")
	}
	return record, nil
}

// shareAllSystems reports whether any authenticated user can see all systems
// (mirrors the SHARE_ALL_SYSTEMS env var behavior used by collection rules).
func shareAllSystems() bool {
	value, _ := utils.GetEnv("SHARE_ALL_SYSTEMS")
	return value == "true"
}

// resolveSystem finds a system by name or id, restricted to systems visible
// to the auth record. An exact id match takes precedence over name matches; a
// name matching multiple visible systems is rejected instead of being resolved
// to an arbitrary match.
func (h *mcphub) resolveSystem(auth *core.Record, system string) (*core.Record, error) {
	notFound := fmt.Errorf("system %q "+notVisibleMsg, system)
	records, err := h.app.FindRecordsByFilter("systems", "name = {:s} || id = {:s}", "", maxListRecords, 0, dbx.Params{"s": system})
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, notFound
	}
	// exact id match wins over name matches
	for _, record := range records {
		if record.Id == system {
			return h.visibleSystem(auth, record, notFound)
		}
	}
	// name matches: only consider systems visible to the user
	visible := make([]*core.Record, 0, len(records))
	for _, record := range records {
		if _, err := h.visibleSystem(auth, record, notFound); err == nil {
			visible = append(visible, record)
		}
	}
	switch {
	case len(visible) == 0:
		return nil, notFound
	case len(visible) > 1:
		return nil, fmt.Errorf("system name %q is ambiguous (%d visible systems match); use the system id instead", system, len(visible))
	}
	return visible[0], nil
}

// visibleSystem returns the system if it is visible to the auth record.
func (h *mcphub) visibleSystem(auth *core.Record, record *core.Record, notFound error) (*core.Record, error) {
	if auth.IsSuperuser() || shareAllSystems() || slices.Contains(record.GetStringSlice("users"), auth.Id) {
		return record, nil
	}
	return nil, notFound
}
