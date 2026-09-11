package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// sessionAs seeds a signed-in session holding a role, which is what the
// enforcement middleware reads.
func sessionAs(st *fakeStore, role authz.Role) *fakeStore {
	st.sessions[testSessionID] = models.Session{
		ID: testSessionID, Subject: "tester", Name: "tester", Source: "oidc", Role: string(role),
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}
	return st
}

// asRole makes a request carrying the session cookie but no basic auth: basic
// auth is the local administrator, and would mask what the session may do.
func asRole(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestRolesDecideWhatARequestMayDo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       authz.Role
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "a viewer may read the templates", role: authz.RoleViewer, method: http.MethodGet, path: "/api/templates", wantStatus: http.StatusOK},
		{name: "a viewer may read the page", role: authz.RoleViewer, method: http.MethodGet, path: "/admin", wantStatus: http.StatusOK},
		{name: "a viewer may see the routing graph", role: authz.RoleViewer, method: http.MethodGet, path: "/api/routing/graph", wantStatus: http.StatusOK},
		{
			// Asking which route an alert would take changes nothing, so it is a
			// read that happens to need a body.
			name: "a viewer may probe a match", role: authz.RoleViewer, method: http.MethodPost,
			path: "/api/routing/match", body: `{"labels":{}}`, wantStatus: http.StatusOK,
		},
		{
			name: "a viewer may preview a template", role: authz.RoleViewer, method: http.MethodPost,
			path: "/api/templates/preview", body: `{"body":"{}"}`, wantStatus: http.StatusOK,
		},
		{
			name: "a viewer may not create a template", role: authz.RoleViewer, method: http.MethodPost,
			path: "/api/templates", body: `{"name":"x","body":"{}"}`, wantStatus: http.StatusForbidden,
		},
		{
			name: "a viewer may not delete a route", role: authz.RoleViewer, method: http.MethodDelete,
			path: "/api/routes/known", wantStatus: http.StatusForbidden,
		},
		{
			name: "an editor may create a template", role: authz.RoleEditor, method: http.MethodPost,
			path: "/api/templates", body: `{"name":"x","body":"{}"}`, wantStatus: http.StatusCreated,
		},
		{
			name: "an admin may create a template", role: authz.RoleAdmin, method: http.MethodPost,
			path: "/api/templates", body: `{"name":"x","body":"{}"}`, wantStatus: http.StatusCreated,
		},
		{
			// Either tampering or a session from a newer build; neither is a
			// reason to let it through.
			name: "an unknown role may do nothing", role: authz.Role("superuser"), method: http.MethodGet,
			path: "/api/templates", wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), tt.role)
			st.routes["known"] = models.Route{ID: "known", Name: "known"}
			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.method, tt.path, tt.body)

			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s as %s = %d, want %d (%s)", tt.method, tt.path, tt.role, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// A refusal has to say what was refused; "403" alone leaves an operator
// guessing whether they are signed in at all.
func TestRefusalNamesWhatWasRefused(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	api := asRole(t, handler, http.MethodPost, "/api/destinations", `{"name":"x"}`)
	if !strings.Contains(api.Body.String(), "viewer") || !strings.Contains(api.Body.String(), "destination") {
		t.Errorf("API refusal = %q, want it to name the role and the resource", api.Body.String())
	}

	form := asRole(t, handler, http.MethodPost, "/admin/destinations", "name=x")
	if form.Code != http.StatusForbidden {
		t.Errorf("form post as a viewer = %d, want 403", form.Code)
	}
}

// Hiding a control is not enforcing anything, so the form post is refused on
// its own — but the control is hidden too, because offering a viewer a button
// that always fails is its own kind of broken.
func TestViewerSeesNoControls(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleViewer)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()

	for _, unwanted := range []string{
		`action="/admin/templates"`, `action="/admin/routes"`, `action="/admin/templates/delete"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the page still offers %q to a viewer", unwanted)
		}
	}
	if !strings.Contains(body, "may look but not change anything") {
		t.Error("the page does not say why the controls are missing")
	}
	// The lists themselves are the point of being a viewer.
	if !strings.Contains(body, "Critical to ops") {
		t.Error("a viewer cannot see the configuration at all")
	}
}

func TestEditorSeesControls(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()

	if !strings.Contains(body, `action="/admin/templates"`) {
		t.Error("an editor is not offered the template form")
	}
	if strings.Contains(body, "may look but not change anything") {
		t.Error("an editor is told they are read-only")
	}
}

// A session written before roles existed carries none. Demoting it mid-shift
// would take away access the operator already had.
func TestSessionWithoutARoleKeepsItsAccess(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.Role(""))
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/templates", `{"name":"x","body":"{}"}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("POST as a pre-roles session = %d, want it to still work (%s)", rec.Code, rec.Body.String())
	}
}

// The API credentials are the local administrator's, and scripts using them
// predate roles entirely.
func TestBasicAuthAdministers(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(`{"name":"x","body":"{}"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "pass")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("POST with basic auth = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
}

func TestRequestAuthorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method       string
		path         string
		wantAction   string
		wantResource string
	}{
		{method: http.MethodGet, path: "/admin", wantAction: authz.ActionView, wantResource: "Page"},
		{method: http.MethodGet, path: "/api/templates", wantAction: authz.ActionView, wantResource: "Template"},
		{method: http.MethodPut, path: "/api/templates/abc", wantAction: authz.ActionEdit, wantResource: "Template"},
		{method: http.MethodPost, path: "/admin/destinations/delete", wantAction: authz.ActionEdit, wantResource: "Destination"},
		{method: http.MethodPost, path: "/admin/routes", wantAction: authz.ActionEdit, wantResource: "Route"},
		{method: http.MethodGet, path: "/api/graph/teams", wantAction: authz.ActionView, wantResource: "Directory"},
		{method: http.MethodPost, path: "/api/routing/match", wantAction: authz.ActionView, wantResource: "Routing"},
		{method: http.MethodPost, path: "/api/templates/preview", wantAction: authz.ActionView, wantResource: "Template"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			action, resource := requestAuthorization(httptest.NewRequest(tt.method, tt.path, nil))
			if action != tt.wantAction || resource.Type != tt.wantResource {
				t.Errorf("requestAuthorization() = %q on %q, want %q on %q", action, resource.Type, tt.wantAction, tt.wantResource)
			}
		})
	}
}

// The form path posts urlencoded bodies, so the origin guard and the role check
// have to agree about a viewer: refused, and not because of the origin.
func TestViewerFormPostIsRefusedOnItsRole(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	form := url.Values{"name": {"New"}, "body": {"{}"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/templates", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST = %d, want 403", rec.Code)
	}
	if len(st.templates) != 1 {
		t.Errorf("stored %d templates, want the viewer's post refused", len(st.templates))
	}
}

// The admin UI fetches /api itself, so a browser signed in through the provider
// has to reach it without the local credentials — and a cookie-authenticated
// write still has to prove where it came from.
func TestAPIAcceptsASessionButGuardsItsOrigin(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	if rec := asRole(t, handler, http.MethodGet, "/api/templates", ""); rec.Code != http.StatusOK {
		t.Errorf("GET with a session = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(`{"name":"x","body":"{}"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site POST with a session = %d, want 403", rec.Code)
	}
	if len(st.templates) != 0 {
		t.Error("the cross-site post reached the store")
	}
}

// No session and no credentials is still a 401 asking for them, not a 403.
func TestAPIWithoutAnyCredentials(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without credentials = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("the 401 does not say how to authenticate")
	}
}
