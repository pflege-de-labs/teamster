package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestStartSessionRecordsTheUser(t *testing.T) {
	t.Parallel()

	disabledAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		existing *models.User
		source   string
		failOn   string
		wantErr  error
		wantUser bool
	}{
		{name: "a first sign-in", source: "oidc", wantUser: true},
		{name: "a disabled user is refused", existing: &models.User{Subject: "s-1", Source: "oidc", DisabledAt: disabledAt}, source: "oidc", wantErr: errUserDisabled},
		{name: "the local login is never refused", existing: &models.User{Subject: "s-1", Source: "local", DisabledAt: disabledAt}, source: sourceLocal, wantUser: true},
		{name: "a failing registry write still signs in", source: "oidc", failOn: "RecordSignIn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := newFakeStore()
			delete(st.sessions, testSessionID)
			if tt.existing != nil {
				st.users[tt.existing.Subject] = *tt.existing
			}
			if tt.failOn != "" {
				st.fail(tt.failOn)
			}
			s := &Server{store: st}
			identity := models.Identity{Email: "a@example.com", Groups: []string{"sre"}}

			rec := httptest.NewRecorder()
			_, err := s.startSession(rec, httptest.NewRequest(http.MethodGet, "/", nil), "s-1", "Alice", tt.source, []authz.Role{authz.RoleEditor}, identity)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("startSession() = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(st.sessions) != 0 {
					t.Error("a refused sign-in started a session")
				}
				return
			}
			if len(st.sessions) != 1 {
				t.Errorf("sessions = %d, want 1", len(st.sessions))
			}
			if !tt.wantUser {
				return
			}
			u := st.users["s-1"]
			if u.Name != "Alice" || u.Email != "a@example.com" || len(u.Roles) != 1 || u.Roles[0] != "editor" || len(u.IdPGroups) != 1 {
				t.Errorf("recorded user = %+v", u)
			}
		})
	}

	st := newFakeStore().fail("GetUser")
	s := &Server{store: st}
	if _, err := s.startSession(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), "s-1", "", "oidc", nil, models.Identity{}); err == nil {
		t.Error("a failing user lookup did not stop the sign-in")
	}
}

func usersStore() *fakeStore {
	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.users["s-bob"] = models.User{Subject: "s-bob", Source: "oidc", Name: "Bob", Roles: []string{"viewer"}, IdPGroups: []string{"sre"}}
	st.users["admin"] = models.User{Subject: "admin", Source: "local", Name: "admin"}
	st.users["tester"] = models.User{Subject: "tester", Source: "oidc", Name: "tester"}
	st.sessions["bobs"] = models.Session{ID: "bobs", Subject: "s-bob", Source: "oidc", ExpiresAt: time.Now().Add(time.Hour)}
	return st
}

func TestUsersPage(t *testing.T) {
	t.Parallel()

	st := usersStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	body := asRole(t, handler, http.MethodGet, "/admin/users", "").Body.String()
	for _, want := range []string{"Bob", "s-bob", "sre", `action="/admin/users/disable"`, `href="/admin/users"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Neither the local login nor the viewer themself gets a button.
	if n := strings.Count(body, `name="subject"`); n != 1 {
		t.Errorf("%d disable buttons, want only Bob's", n)
	}
	if body := asRole(t, handler, http.MethodGet, "/admin/users?q=nobody", "").Body.String(); !strings.Contains(body, "nobody matches") {
		t.Error("an empty search does not say so")
	}

	rec := postFormAs(t, handler, "/admin/users/disable", url.Values{"subject": {"s-bob"}})
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("disable = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if u := st.users["s-bob"]; !u.Disabled() || u.DisabledBy != "tester" {
		t.Errorf("after disable: %+v", u)
	}
	if _, ok := st.sessions["bobs"]; ok {
		t.Error("the disabled user's session survived")
	}
	if body := asRole(t, handler, http.MethodGet, "/admin/users", "").Body.String(); !strings.Contains(body, "Disabled by tester") || !strings.Contains(body, `action="/admin/users/enable"`) {
		t.Error("the page does not show who disabled Bob, or offer to enable")
	}

	rec = postFormAs(t, handler, "/admin/users/enable", url.Values{"subject": {"s-bob"}})
	if rec.Code != http.StatusSeeOther || st.users["s-bob"].Disabled() {
		t.Errorf("enable = %d, user %+v", rec.Code, st.users["s-bob"])
	}

	refusals := []struct {
		name, subject, want string
	}{
		{"yourself", "tester", "cannot+disable+yourself"},
		{"the local login", "admin", "local+login"},
		{"someone who never signed in", "ghost", "no+user"},
	}
	for _, tt := range refusals {
		rec := postFormAs(t, handler, "/admin/users/disable", url.Values{"subject": {tt.subject}})
		if !strings.Contains(rec.Header().Get("Location"), tt.want) {
			t.Errorf("disabling %s redirects to %q, want an error naming %s", tt.name, rec.Header().Get("Location"), tt.want)
		}
	}

	if rec := asRole(t, handler, http.MethodPost, "/admin/users", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/users = %d, want 405", rec.Code)
	}
	failing := newTestServer(t, sessionAs(newFakeStore(), authz.RoleAdmin).fail("ListUsers"), &fakeMessenger{}).Handler
	if body := asRole(t, failing, http.MethodGet, "/admin/users", "").Body.String(); !strings.Contains(body, "Something went wrong") {
		t.Error("a failing store is not reported on the page")
	}
}

func TestUsersAPI(t *testing.T) {
	t.Parallel()

	st := usersStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := asRole(t, handler, http.MethodGet, "/api/users?q=bob&limit=5", "")
	var users []models.User
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil || len(users) != 1 || users[0].Subject != "s-bob" {
		t.Fatalf("GET /api/users = %d %s", rec.Code, rec.Body.String())
	}

	tests := []struct {
		name, method, body string
		want               int
	}{
		{"disable", http.MethodPost, `{"subject":"s-bob"}`, http.StatusOK},
		{"enable", http.MethodDelete, `{"subject":"s-bob"}`, http.StatusOK},
		{"unknown", http.MethodPost, `{"subject":"ghost"}`, http.StatusNotFound},
		{"self", http.MethodPost, `{"subject":"tester"}`, http.StatusConflict},
		{"local", http.MethodPost, `{"subject":"admin"}`, http.StatusConflict},
		{"no body", http.MethodPost, ``, http.StatusBadRequest},
		{"wrong method", http.MethodGet, ``, http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, "/api/users/disabled", strings.NewReader(tt.body))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s: %d %s, want %d", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}

	for _, failOn := range []string{"ListUsers"} {
		h := newTestServer(t, sessionAs(newFakeStore(), authz.RoleAdmin).fail(failOn), &fakeMessenger{}).Handler
		if rec := asRole(t, h, http.MethodGet, "/api/users", ""); rec.Code != http.StatusInternalServerError {
			t.Errorf("failing %s = %d, want 500", failOn, rec.Code)
		}
	}
	if rec := asRole(t, newTestServer(t, sessionAs(newFakeStore(), authz.RoleAdmin), &fakeMessenger{}).Handler, http.MethodGet, "/api/users", ""); rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Errorf("no users = %q, want []", rec.Body.String())
	}
	if rec := asRole(t, handler, http.MethodPost, "/api/users", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/users = %d, want 405", rec.Code)
	}
}

func TestUsersAreTheAdminsAlone(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/admin/users", "/api/users"} {
		handler := newTestServer(t, sessionAs(newFakeStore(), authz.RoleEditor), &fakeMessenger{}).Handler
		if rec := asRole(t, handler, http.MethodGet, path, ""); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as editor = %d, want 403", path, rec.Code)
		}
	}
}
