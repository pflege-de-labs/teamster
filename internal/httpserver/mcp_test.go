package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// mcpTestServer returns the Server itself, which NewServer hides behind *http.Server.
func mcpTestServer(t *testing.T, st *fakeStore, enabled bool) (*Server, *http.Server) {
	t.Helper()
	var api *Server
	cfg := config.Config{
		Server: config.ServerConfig{Addr: ":0"},
		Admin:  config.AdminConfig{Username: "admin", Password: "pass"},
		MCP:    config.MCPConfig{Enabled: enabled},
	}
	srv, err := NewServer(quietLog, cfg, st, &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), nil, func(s *Server) { api = s })
	if err != nil {
		t.Fatal(err)
	}
	return api, srv
}

func mcpCtx(access string) context.Context {
	ctx := withPrincipal(context.Background(), "alice", "Alice", models.ViaMCP, []authz.Role{authz.RoleEditor}, nil)
	return context.WithValue(ctx, mcpTokenKey, models.MCPToken{Name: "ci", Access: access})
}

func TestCallAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		access     string
		method     string
		path       string
		body       any
		wantStatus int
		wantBody   string
		wantErr    bool
		wantCreate bool
	}{
		{name: "list", access: models.MCPAccessWrite, method: "GET", path: "/api/templates", wantStatus: 200, wantBody: "tpl-1"},
		{name: "escaped id stays one segment", access: models.MCPAccessWrite, method: "GET", path: apiPath("/api/templates/%s", "../routes"), wantStatus: 404},
		{name: "read-only create refused", access: models.MCPAccessRead, method: "POST", path: "/api/templates", body: map[string]string{"name": "x", "body": "{}"}, wantStatus: 403},
		{name: "write create served", access: models.MCPAccessWrite, method: "POST", path: "/api/templates", body: map[string]string{"name": "x", "body": "{}"}, wantStatus: 201, wantCreate: true},
		{name: "read-only match served", access: models.MCPAccessRead, method: "POST", path: "/api/routing/match", body: map[string]any{"labels": map[string]string{}}, wantStatus: 200},
		{name: "read-only preview served", access: models.MCPAccessRead, method: "POST", path: "/api/templates/preview", body: map[string]string{"body": "{}"}, wantStatus: 200},
		{name: "admin page refused", access: models.MCPAccessWrite, method: "POST", path: "/admin/tokens/mcp/new", wantErr: true},
		{name: "admin listing refused", access: models.MCPAccessWrite, method: "GET", path: "/admin/tokens", wantErr: true},
		{name: "relative path refused", access: models.MCPAccessWrite, method: "GET", path: "api/templates", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			st.templates["tpl-1"] = models.Template{ID: "tpl-1", Name: "one", Body: "{}"}
			api, _ := mcpTestServer(t, st, true)

			status, body, err := api.callAPI(mcpCtx(tt.access), tt.method, tt.path, tt.body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", status, tt.wantStatus, body)
			}
			if !strings.Contains(string(body), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %q", body, tt.wantBody)
			}
			if _, created := st.templates["generated"]; created != tt.wantCreate {
				t.Errorf("store saw create = %v, want %v", created, tt.wantCreate)
			}
		})
	}
}

func TestAPIPath(t *testing.T) {
	t.Parallel()

	got := apiPath("/api/templates/%s/x/%s", "../routes", "a b")
	if want := "/api/templates/..%2Froutes/x/a%20b"; got != want {
		t.Errorf("apiPath = %q, want %q", got, want)
	}
}

func TestAPIResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    bool
		wantText   string
		wantStruct any
	}{
		{name: "object", status: 200, body: `{"a":1}`, wantStruct: map[string]any{"a": float64(1)}},
		{name: "list is wrapped", status: 200, body: `[1]`, wantStruct: map[string]any{"items": []any{float64(1)}}},
		{name: "no content", status: 204},
		{name: "client error", status: 403, body: `{"error":"nope"}`, wantErr: true, wantText: "nope"},
		{name: "plain client error", status: 400, body: "bad\n", wantErr: true, wantText: "bad"},
		{name: "server error", status: 500, body: `{"error":"db password leaked"}`, wantErr: true, wantText: mcpServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := apiResult(t.Context(), tt.status, []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != tt.wantErr {
				t.Errorf("IsError = %v, want %v", res.IsError, tt.wantErr)
			}
			if tt.wantText != "" {
				text, ok := res.Content[0].(*mcp.TextContent)
				if !ok || text.Text != tt.wantText {
					t.Errorf("content = %v, want text %q", res.Content, tt.wantText)
				}
			}
			if tt.wantStruct != nil && !reflect.DeepEqual(res.StructuredContent, tt.wantStruct) {
				t.Errorf("structured = %v, want %v", res.StructuredContent, tt.wantStruct)
			}
			if tt.wantStruct == nil && res.StructuredContent != nil {
				t.Errorf("structured = %v, want none", res.StructuredContent)
			}
		})
	}
}

func TestMCPEndpointRegistration(t *testing.T) {
	t.Parallel()

	const secret = "tmcp_secret"
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	post := func(srv *http.Server, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initialize))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		return rec
	}
	st := newFakeStore()
	st.users["alice"] = models.User{Subject: "alice", Name: "Alice", Source: "oidc", Roles: []string{"editor"}}
	mustCreate(t, st, models.MCPToken{Name: "ci", TokenHash: hashToken(secret), CreatedBy: "alice", Access: models.MCPAccessRead})

	_, off := mcpTestServer(t, st, false)
	rec := post(off, secret)
	if rec.Header().Get("WWW-Authenticate") == `Bearer realm="mcp"` || strings.Contains(rec.Body.String(), "teamster") {
		t.Errorf("disabled: served by MCP: %d %s", rec.Code, rec.Body)
	}
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("disabled: %d %q, want the catch-all's login redirect", rec.Code, rec.Header().Get("Location"))
	}

	_, on := mcpTestServer(t, st, true)
	if rec := post(on, ""); rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != `Bearer realm="mcp"` {
		t.Errorf("no token: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	rec = post(on, secret)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), mcpInstructions[:30]) {
		t.Errorf("valid token: %d %s", rec.Code, rec.Body)
	}
}
