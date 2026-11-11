package httpserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestMCPAuth(t *testing.T) {
	t.Parallel()

	const secret = "tmcp_secret"
	alice := models.User{Subject: "alice", Name: "Alice", Source: "oidc", Roles: []string{"editor"}}
	tests := []struct {
		name       string
		users      []models.User
		admin      bool
		header     func(r *http.Request)
		failOn     string
		wantStatus int
		wantBody   string
		wantErr    string
	}{
		{name: "no credential", wantStatus: 401},
		{name: "session cookie only", header: func(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID}) }, wantStatus: 401},
		{name: "basic auth", header: func(r *http.Request) { r.SetBasicAuth("admin", "pass") }, wantStatus: 401},
		{name: "unknown token", users: []models.User{alice}, header: bearer("tmcp_nope"), wantStatus: 401},
		{name: "webhook token", users: []models.User{alice}, header: bearer("tst_hook"), wantStatus: 401},
		{name: "X-Teamster-Token is not accepted", users: []models.User{alice}, header: func(r *http.Request) { r.Header.Set("X-Teamster-Token", secret) }, wantStatus: 401},
		{name: "editor creator", users: []models.User{alice}, header: bearer(secret), wantStatus: 200, wantBody: "alice|Alice|editor|mcp"},
		{name: "scheme case and padding", users: []models.User{alice}, header: func(r *http.Request) { r.Header.Set("Authorization", "bearer  "+secret+" ") }, wantStatus: 200, wantBody: "alice|Alice|editor|mcp"},
		{name: "creator disabled", users: []models.User{{Subject: "alice", Source: "oidc", DisabledAt: time.Now()}}, header: bearer(secret), wantStatus: 403, wantErr: "creator is disabled or unknown"},
		{name: "creator unknown", header: bearer(secret), wantStatus: 403, wantErr: "creator is disabled or unknown"},
		{name: "local admin without a row", admin: true, header: bearer(secret), wantStatus: 200, wantBody: "admin|admin|admin|mcp"},
		{name: "token lookup fails", users: []models.User{alice}, header: bearer(secret), failOn: "GetMCPTokenByHash", wantStatus: 503},
		{name: "creator lookup fails", users: []models.User{alice}, header: bearer(secret), failOn: "GetUser", wantStatus: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			created := "alice"
			if tt.admin {
				created = "admin"
			}
			for _, u := range tt.users {
				st.users[u.Subject] = u
			}
			mustCreate(t, st, models.MCPToken{Name: "ci", TokenHash: hashToken(secret), CreatedBy: created, Access: models.MCPAccessWrite})
			if _, err := st.CreateAccessToken(t.Context(), models.AccessToken{Name: "hook", TokenHash: hashToken("tst_hook"), CreatedBy: "alice"}); err != nil {
				t.Fatal(err)
			}
			if tt.failOn != "" {
				st.failOn[tt.failOn] = true
			}

			srv := &Server{store: st, cfg: config.Config{Admin: config.AdminConfig{Username: "admin", Password: "pass"}}}
			echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				subject, roles := principalOf(r)
				name, _ := r.Context().Value(nameKey).(string)
				names := make([]string, len(roles))
				for i, role := range roles {
					names[i] = string(role)
				}
				fmt.Fprintf(w, "%s|%s|%s|%s", subject, name, strings.Join(names, ","), actorVia(r))
			})
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.header != nil {
				tt.header(req)
			}
			rec := httptest.NewRecorder()
			srv.mcpAuth(echo).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body, tt.wantBody)
			}
			if !strings.Contains(rec.Body.String(), tt.wantErr) {
				t.Errorf("body = %q, want it to contain %q", rec.Body, tt.wantErr)
			}
			wantAuth := ""
			if tt.wantStatus == 401 {
				wantAuth = `Bearer realm="mcp"`
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != wantAuth {
				t.Errorf("WWW-Authenticate = %q, want %q", got, wantAuth)
			}
		})
	}
}

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func mustCreate(t *testing.T, st *fakeStore, tok models.MCPToken) {
	t.Helper()
	if _, err := st.CreateMCPToken(t.Context(), tok); err != nil {
		t.Fatal(err)
	}
}

func actorVia(r *http.Request) string {
	return audit.ActorFrom(r.Context()).Via
}

func TestMCPWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		readOnly bool
		access   string
		want     bool
	}{
		{"write token", false, models.MCPAccessWrite, true},
		{"read token", false, models.MCPAccessRead, false},
		{"read-only deployment", true, models.MCPAccessWrite, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got bool
			st := newFakeStore()
			st.users["alice"] = models.User{Subject: "alice", Source: "oidc"}
			mustCreate(t, st, models.MCPToken{TokenHash: hashToken("tmcp_x"), CreatedBy: "alice", Access: tt.access})
			cfg := config.MCPConfig{ReadOnly: tt.readOnly}
			srv := &Server{store: st}
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			bearer("tmcp_x")(req)
			srv.mcpAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = mcpWrites(r.Context(), cfg)
			})).ServeHTTP(httptest.NewRecorder(), req)
			if got != tt.want {
				t.Errorf("mcpWrites = %v, want %v", got, tt.want)
			}
		})
	}
	if mcpWrites(t.Context(), config.MCPConfig{}) {
		t.Error("mcpWrites without an admitted token = true")
	}
}

func TestMCPTokenDoesNotOpenOtherDoors(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.users["alice"] = models.User{Subject: "alice", Source: "oidc", Roles: []string{"admin"}}
	mustCreate(t, st, models.MCPToken{TokenHash: hashToken("tmcp_x"), CreatedBy: "alice", Access: models.MCPAccessWrite})
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	for _, path := range []string{"/webhook/universal", "/api/templates"} {
		method := http.MethodPost
		if strings.HasPrefix(path, "/api/") {
			method = http.MethodGet
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{"labels":{"a":"b"}}`))
		bearer("tmcp_x")(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with an MCP token = %d, want 401", path, rec.Code)
		}
	}
}

func TestNewMCPToken(t *testing.T) {
	t.Parallel()

	a, err := newMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newMCPToken()
	if !strings.HasPrefix(a, mcpTokenPrefix) || len(a) != len(mcpTokenPrefix)+64 || a == b {
		t.Errorf("tokens %q, %q: want distinct, %s-prefixed, 256 bits", a, b, mcpTokenPrefix)
	}
}

// The snippet must point at the admin host: a split deployment's webhook host routes only the webhook paths.
func TestMCPURLIsOnTheAdminHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		externalURL string
		want        string
	}{
		{name: "the host the admin UI is read on", want: "http://example.com/mcp"},
		{name: "the configured admin URL", externalURL: "https://admin.example.org/", want: "https://admin.example.org/mcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			cfg.MCP.Enabled = true
			cfg.Server.WebhookURL = "https://hooks.example.org"
			cfg.Server.ExternalURL = tt.externalURL
			h := mustServer(t, cfg, sessionAs(tokenStore(), authz.RoleEditor), &fakeMessenger{}).Handler
			body := postFormAs(t, h, "/admin/tokens/mcp/new", url.Values{"name": {"laptop"}, "access": {"read"}}).Body.String()
			if !strings.Contains(body, "claude mcp add --transport http teamster "+tt.want+" ") {
				t.Errorf("snippet does not point at %s", tt.want)
			}
			if strings.Contains(body, "hooks.example.org/mcp") {
				t.Error("snippet points at the webhook host")
			}
		})
	}
}

// An MCP client probes for OAuth metadata after a 401; a login redirect would read as a broken server.
func TestWellKnownIsNotFound(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.MCP.Enabled = true
	h := mustServer(t, cfg, newFakeStore(), &fakeMessenger{}).Handler
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
			t.Errorf("GET %s = %d Location %q, want 404 without a redirect", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
