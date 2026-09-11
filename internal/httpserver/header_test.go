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
		name  string
		who   string
		roles []authz.Role
		path  string
		want  []string
	}{
		{
			name: "an editor on the configuration page", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin", want: []string{"jens", "editor"},
		},
		{
			name: "and on the routing page", who: "jens", roles: []authz.Role{authz.RoleEditor},
			path: "/admin/routing", want: []string{"jens", "editor"},
		},
		{
			// Several roles are read out as they are; the policies decide, and
			// the header does not pretend one of them wins.
			name: "several roles", who: "jens", roles: []authz.Role{authz.RoleAdmin, "auditor"},
			path: "/admin", want: []string{"jens", "admin, auditor"},
		},
		{
			// A provider that sends no name still signed somebody in.
			name: "a provider that sent no name", roles: []authz.Role{authz.RoleViewer},
			path: "/admin", want: []string{"Signed in", "viewer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := signedInAs(seededUIStore(), tt.who, tt.roles...)
			body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, tt.path, "").Body.String()

			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("the header does not show %q", want)
				}
			}
			if !strings.Contains(body, `action="/admin/logout"`) {
				t.Error("the header offers no way to sign out")
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

	if strings.Contains(rec.Body.String(), "Sign out") {
		t.Error("the login page offers to sign out")
	}
}
