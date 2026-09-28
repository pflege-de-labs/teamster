package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
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
			path: "/admin", want: []string{"jens", "Editor"},
		},
		{
			name: "and on the routing page", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin/routing", want: []string{"jens", "Editor"},
		},
		{
			// Roles nest, so the highest built-in one says it all; a role the
			// deployment defined is on the user info page instead.
			name: "several roles", who: "jens", roles: []authz.Role{"auditor", authz.RoleViewer, authz.RoleAdmin},
			path: "/admin", want: []string{"jens", "Administrator"}, unwanted: []string{"auditor"},
		},
		{
			// Keycloak puts its own roles into the same claim.
			name: "provider roles beside the application role", who: "jens",
			roles: []authz.Role{"offline_access", "uma_authorization", authz.RoleViewer},
			path:  "/admin", want: []string{"Viewer"}, unwanted: []string{"offline_access", "uma_authorization"},
		},
		{
			name: "translated", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin", language: "de", want: []string{"Bearbeiter", "Abmelden"},
		},
		{
			// A provider that sends no name still signed somebody in.
			name: "a provider that sent no name", roles: []authz.Role{authz.RoleViewer},
			path: "/admin", want: []string{"Signed in", "Viewer"},
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
			want:  []string{`href="/admin"`, `href="/admin/routing"`, `href="/admin/recipients"`, `href="/admin/permissions"`},
		},
		{
			name:     "an editor does not",
			roles:    []authz.Role{authz.RoleEditor},
			want:     []string{`href="/admin"`, `href="/admin/routing"`, `href="/admin/recipients"`},
			unwanted: []string{`href="/admin/permissions"`, `href="/admin/notifications"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := signedInAs(seededUIStore(), "jens", tt.roles...)
			body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()

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
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()

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
