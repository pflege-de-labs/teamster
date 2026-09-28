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

func TestUserInfoPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		session  models.Session
		method   string
		status   int
		want     []string
		unwanted []string
	}{
		{
			name: "an oidc session shows what the provider sent",
			session: models.Session{
				Subject: "8f2a", Name: "jens", Source: "oidc",
				Roles: authz.Encode([]authz.Role{"offline_access", authz.RoleEditor}),
				Identity: models.Identity{
					Issuer: "https://idp.example/realms/internal", Email: "jens@example.com",
					Scopes: []string{"openid", "profile"}, Groups: []string{"/ops"}, GroupsSource: "userinfo",
					ClaimValues: []string{"offline_access", "editor"}, ClaimSource: "the access token",
				},
			},
			method: http.MethodGet, status: http.StatusOK,
			want: []string{
				"8f2a", "jens@example.com", "https://idp.example/realms/internal", "openid", "profile", "/ops",
				"offline_access", "Found in the access token.", "Found in the userinfo response.", "Editor",
			},
			unwanted: []string{"Sign out and in again"},
		},
		{
			// A user with no role is who most needs to see why.
			name:    "a user with no role may read it",
			session: models.Session{Subject: "8f2a", Name: "jens", Source: "oidc", Roles: authz.Encode(nil), Identity: models.Identity{Scopes: []string{"openid"}}},
			method:  http.MethodGet, status: http.StatusOK,
			want: []string{"8f2a", "no role"},
		},
		{
			name:    "a session from before identities were kept",
			session: models.Session{Subject: "8f2a", Name: "jens", Source: "oidc", Roles: authz.Encode([]authz.Role{authz.RoleViewer})},
			method:  http.MethodGet, status: http.StatusOK,
			want: []string{"Sign out and in again"},
		},
		{
			name:    "a local login has no provider details",
			session: models.Session{Subject: "admin", Name: "admin", Source: "local", Roles: authz.Encode([]authz.Role{authz.RoleAdmin})},
			method:  http.MethodGet, status: http.StatusOK,
			want:     []string{"Local login", "Administrator"},
			unwanted: []string{"Scopes", "Sign out and in again"},
		},
		{
			name:    "it is read only",
			session: models.Session{Subject: "8f2a", Source: "oidc", Roles: authz.Encode([]authz.Role{authz.RoleAdmin})},
			method:  http.MethodPost, status: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seededUIStore()
			tt.session.ID = testSessionID
			tt.session.CreatedAt = time.Now()
			tt.session.ExpiresAt = time.Now().Add(time.Hour)
			st.sessions[testSessionID] = tt.session

			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.method, "/admin/userinfo", "")
			if rec.Code != tt.status {
				t.Fatalf("%s /admin/userinfo = %d, want %d", tt.method, rec.Code, tt.status)
			}
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("the page does not show %q", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(body, unwanted) {
					t.Errorf("the page shows %q", unwanted)
				}
			}
		})
	}
}

func TestUserInfoPageNeedsASession(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/userinfo", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("GET /admin/userinfo without a session = %d to %q, want a redirect to the login page", rec.Code, rec.Header().Get("Location"))
	}
}
