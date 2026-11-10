package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// signedInAs seeds a session with a name as well as roles, which is what the
// header reads.
func signedInAs(st *fakeStore, name string, roles ...authz.Role) *fakeStore {
	st.sessions[testSessionID] = models.Session{
		ID: testSessionID, Subject: "8f2a", Name: name, Source: "oidc", Roles: authz.Encode(roles),
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}
	return st
}

// "Why is the button missing?" is otherwise a question only the server can
// answer, so the header says who is signed in and what they may do.
func TestHeaderNamesTheSignedInUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		who      string
		roles    []authz.Role
		path     string
		language string
		want     []string
		unwanted []string
	}{
		{
			name: "an editor on the configuration page", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin/templates", want: []string{"jens", "Editor"},
		},
		{
			name: "and on the routing page", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin/routing", want: []string{"jens", "Editor"},
		},
		{
			// Roles nest, so the highest built-in one says it all; a role the
			// deployment defined is on the user info page instead.
			name: "several roles", who: "jens", roles: []authz.Role{"auditor", authz.RoleViewer, authz.RoleAdmin},
			path: "/admin/templates", want: []string{"jens", "Administrator"}, unwanted: []string{"auditor"},
		},
		{
			// Keycloak puts its own roles into the same claim.
			name: "provider roles beside the application role", who: "jens",
			roles: []authz.Role{"offline_access", "uma_authorization", authz.RoleViewer},
			path:  "/admin/templates", want: []string{"Viewer"}, unwanted: []string{"offline_access", "uma_authorization"},
		},
		{
			name: "translated", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin/templates", language: "de", want: []string{"Bearbeiter", "Abmelden"},
		},
		{
			// A provider that sends no name still signed somebody in.
			name: "a provider that sent no name", roles: []authz.Role{authz.RoleViewer},
			path: "/admin/templates", want: []string{"Signed in", "Viewer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := signedInAs(seededUIStore(), tt.who, tt.roles...)
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			var body string
			if tt.language != "" {
				body = pageIn(t, handler, tt.path, tt.language)
			} else {
				body = asRole(t, handler, http.MethodGet, tt.path, "").Body.String()
			}

			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("the header does not show %q", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(body, unwanted) {
					t.Errorf("the header shows %q", unwanted)
				}
			}
			for _, want := range []string{`action="/admin/logout"`, `href="/admin/userinfo"`, `id="language-flags"`, `id="user-menu"`} {
				if !strings.Contains(body, want) {
					t.Errorf("the user menu lacks %s", want)
				}
			}
		})
	}
}

// The sidebar offers the pages the viewer may use, and only those.
func TestSidebarLinksThePages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		roles    []authz.Role
		want     []string
		unwanted []string
	}{
		{
			name:  "an admin manages permissions",
			roles: []authz.Role{authz.RoleAdmin},
			want:  []string{`href="/admin/routing"`, `href="/admin/recipients"`, `href="/admin/permissions"`},
		},
		{
			name:     "an editor does not",
			roles:    []authz.Role{authz.RoleEditor},
			want:     []string{`href="/admin/routing"`, `href="/admin/recipients"`},
			unwanted: []string{`href="/admin/permissions"`, `href="/admin/notifications"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := signedInAs(seededUIStore(), "jens", tt.roles...)
			body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

			if !strings.Contains(body, `id="sidebar"`) || !strings.Contains(body, `id="nav-toggle"`) {
				t.Fatal("the page has no sidebar and burger")
			}
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("the sidebar lacks %s", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(body, unwanted) {
					t.Errorf("the sidebar offers %s", unwanted)
				}
			}
		})
	}
}

// The page a user with no role lands on is exactly where the reason has to be
// visible.
func TestHeaderSaysWhenThereIsNoRole(t *testing.T) {
	t.Parallel()

	st := signedInAs(seededUIStore(), "jens")
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

	if !strings.Contains(body, "jens") || !strings.Contains(body, "no role") {
		t.Error("the header does not say that the user holds no role")
	}
}

// Nobody is signed in on the login page, so there is nobody to name.
func TestLoginPageHasNoIdentityBlock(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), authConfigFor("https://idp.example/.well-known/openid-configuration"))

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, unwanted := range []string{"Sign out", `id="sidebar"`, `id="user-menu"`} {
		if strings.Contains(rec.Body.String(), unwanted) {
			t.Errorf("the login page shows %s", unwanted)
		}
	}
	if !strings.Contains(rec.Body.String(), `id="language-picker"`) {
		t.Error("the login page offers no language")
	}
}

// The burger is in the served markup rather than revealed by nav.js at the end
// of the body, so the logo beside it does not move once the page has painted.
func TestBurgerIsRenderedBeforeFirstPaint(t *testing.T) {
	t.Parallel()

	st := signedInAs(seededUIStore(), "jens", authz.RoleEditor)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

	head, _, _ := strings.Cut(body, "</head>")
	if !strings.Contains(head, `classList.add("js")`) || !strings.Contains(head, `"sidebar-closed"`) {
		t.Error("the head does not apply the sidebar state before the body renders")
	}
	toggle := body[strings.Index(body, `id="nav-toggle"`):]
	toggle, _, _ = strings.Cut(toggle, ">")
	if strings.Contains(toggle, " hidden ") || strings.Contains(toggle, " hidden>") {
		t.Errorf("the burger carries the hidden attribute: %s", toggle)
	}
}

// The sidebar footer names the build, and says nothing when there is none.
func TestSidebarShowsTheVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "a stamped build", version: "v1.2.3", want: true},
		{name: "no version", version: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
				Version: tt.version,
			}
			st := signedInAs(seededUIStore(), "jens", authz.RoleEditor)
			body := asRole(t, mustServer(t, cfg, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

			if got := strings.Contains(body, "Version "+tt.version); got != tt.want {
				t.Errorf("the sidebar shows the version: %v, want %v", got, tt.want)
			}
		})
	}
}

// botServer is a server with the bot configured and installed for everyone.
func botServer(t *testing.T, st *fakeStore) *http.Server {
	t.Helper()

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     notificationsBotConfig(),
	}
	cfg.Bot.GlobalInstall = true
	return mustServer(t, cfg, st, &fakeMessenger{})
}

// sidebarOf cuts the sidebar out of a page.
func sidebarOf(body string) string {
	_, rest, _ := strings.Cut(body, `id="sidebar"`)
	side, _, _ := strings.Cut(rest, "</aside>")
	return side
}

func TestSidebarGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		role   authz.Role
		groups []string
		hrefs  []string
		absent []string
	}{
		{
			name:   "an admin sees every group in order",
			role:   authz.RoleAdmin,
			groups: []string{"configuration", "delivery", "access", "administration"},
			hrefs: []string{
				"/admin/routing", "/admin/routes", "/admin/templates", "/admin/destinations", "/admin/webhooks",
				"/admin/teams", "/admin/recipients", "/admin/broadcasts",
				"/admin/access", "/admin/grants", "/admin/permissions", "/admin/groups", "/admin/users", "/admin/tokens",
				"/admin/people",
			},
		},
		{
			name:   "a viewer has no administration or permission pages",
			role:   authz.RoleViewer,
			groups: []string{"configuration", "delivery", "access"},
			absent: []string{`data-group="administration"`, `href="/admin/grants"`, `href="/admin/permissions"`, `href="/admin/users"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := signedInAs(seededUIStore(), "jens", tt.role)
			side := sidebarOf(asRole(t, botServer(t, st).Handler, http.MethodGet, "/admin/templates", "").Body.String())

			if got := strings.Count(side, "data-group="); got != len(tt.groups) {
				t.Errorf("sidebar has %d groups, want %d", got, len(tt.groups))
			}
			last := -1
			for _, g := range tt.groups {
				i := strings.Index(side, `data-group="`+g+`"`)
				if i < 0 || i < last {
					t.Errorf("group %s missing or out of order", g)
				}
				last = i
			}
			last = -1
			for _, h := range tt.hrefs {
				i := strings.Index(side, `href="`+h+`"`)
				if i < 0 || i < last {
					t.Errorf("link %s missing or out of order", h)
				}
				last = i
			}
			for _, a := range tt.absent {
				if strings.Contains(side, a) {
					t.Errorf("sidebar contains %s", a)
				}
			}
		})
	}
}

func TestSidebarCurrent(t *testing.T) {
	t.Parallel()

	tests := []struct{ path, current string }{
		{"/admin/routes", "/admin/routes"},
		{"/admin/templates", "/admin/templates"},
		{"/admin/routing", "/admin/routing"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			st := signedInAs(seededUIStore(), "jens", authz.RoleAdmin)
			side := sidebarOf(asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, tt.path, "").Body.String())

			if got := strings.Count(side, `aria-current="page"`); got != 1 {
				t.Fatalf("%d links are current, want 1", got)
			}
			i := strings.Index(side, `aria-current="page"`)
			open := strings.LastIndex(side[:i], "<a")
			if !strings.Contains(side[open:i], `href="`+tt.current+`"`) {
				t.Errorf("current link is not %s: %s", tt.current, side[open:i])
			}
		})
	}
}

func TestNotificationsInUserMenu(t *testing.T) {
	t.Parallel()

	st := signedInAs(seededUIStore(), "jens", authz.RoleEditor)
	body := asRole(t, botServer(t, st).Handler, http.MethodGet, "/admin/templates", "").Body.String()

	link := `href="/admin/notifications"`
	if strings.Contains(sidebarOf(body), link) {
		t.Error("the sidebar still links notifications")
	}
	_, menu, _ := strings.Cut(body, `id="user-menu"`)
	if !strings.Contains(menu, link) {
		t.Error("the user menu lacks the notifications link")
	}
}
