package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// recipientStore seeds the session's own chat ("mine", subject "tester", see
// sessionAs) and someone else's ("theirs"), plus a stored personal route for
// each.
func recipientStore(roles ...authz.Role) *fakeStore {
	st := sessionAs(newFakeStore(), roles...)
	st.recipients["mine"] = models.Recipient{ID: "mine", Subject: "tester", Name: "Me"}
	st.recipients["theirs"] = models.Recipient{ID: "theirs", Subject: "someone-else", Name: "Them"}
	st.destinations["dest"] = models.Destination{ID: "dest", Name: "Ops", TeamID: "team", ChannelID: "chan"}
	st.routes["my-route"] = models.Route{ID: "my-route", Name: "mine", RecipientID: "mine"}
	st.routes["their-route"] = models.Route{ID: "their-route", Name: "theirs", RecipientID: "theirs"}
	return st
}

func TestARoutePersonIsYourselfUnlessAdmin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       authz.Role
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name: "an editor may route to their own chat", role: authz.RoleEditor, method: http.MethodPost,
			path: "/api/routes", body: `{"name":"x","recipient_id":"mine"}`, wantStatus: http.StatusCreated,
		},
		{
			name: "an editor may not route to someone else's chat", role: authz.RoleEditor, method: http.MethodPost,
			path: "/api/routes", body: `{"name":"x","recipient_id":"theirs"}`, wantStatus: http.StatusForbidden,
		},
		{
			name: "an admin may route to someone else's chat", role: authz.RoleAdmin, method: http.MethodPost,
			path: "/api/routes", body: `{"name":"x","recipient_id":"theirs"}`, wantStatus: http.StatusCreated,
		},
		{
			// Repointing someone else's route at yourself still rewrites theirs.
			name: "an editor may not rewrite someone else's personal route", role: authz.RoleEditor, method: http.MethodPut,
			path: "/api/routes/their-route", body: `{"name":"x","recipient_id":"mine"}`, wantStatus: http.StatusForbidden,
		},
		{
			name: "an editor may rewrite their own personal route", role: authz.RoleEditor, method: http.MethodPut,
			path: "/api/routes/my-route", body: `{"name":"renamed","recipient_id":"mine"}`, wantStatus: http.StatusOK,
		},
		{
			name: "an editor may not delete someone else's personal route", role: authz.RoleEditor, method: http.MethodDelete,
			path: "/api/routes/their-route", wantStatus: http.StatusForbidden,
		},
		{
			name: "an admin may delete someone else's personal route", role: authz.RoleAdmin, method: http.MethodDelete,
			path: "/api/routes/their-route", wantStatus: http.StatusOK,
		},
		{
			// Nobody is behind an unknown id, so there is nobody to protect.
			name: "a route may name a recipient that does not exist", role: authz.RoleEditor, method: http.MethodPost,
			path: "/api/routes", body: `{"name":"x","recipient_id":"gone"}`, wantStatus: http.StatusCreated,
		},
		{
			name: "a route may not name a channel and a person", role: authz.RoleAdmin, method: http.MethodPost,
			path: "/api/routes", body: `{"name":"x","destination_id":"dest","recipient_id":"mine"}`, wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, recipientStore(tt.role), &fakeMessenger{}).Handler
			if rec := asRole(t, handler, tt.method, tt.path, tt.body); rec.Code != tt.wantStatus {
				t.Errorf("%s %s = %d, want %d (%s)", tt.method, tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// The form's one target select is what makes the two kinds exclusive; the old
// pair of selects always submitted a destination.
func TestTheRouteFormSavesOneTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		role          authz.Role
		target        string
		wantError     string
		wantRecipient string
		wantDest      string
		wantAddressed bool
	}{
		{name: "a channel", role: authz.RoleEditor, target: "destination:dest", wantDest: "dest"},
		{name: "your own chat", role: authz.RoleEditor, target: "recipient:mine", wantRecipient: "mine"},
		{name: "someone else's chat", role: authz.RoleEditor, target: "recipient:theirs", wantError: "your own chat"},
		{name: "someone else's chat as admin", role: authz.RoleAdmin, target: "recipient:theirs", wantRecipient: "theirs"},
		{name: "an unknown kind", role: authz.RoleAdmin, target: "team:dest", wantError: "unknown route target"},
		{name: "the people a message names", role: authz.RoleAdmin, target: "addressed", wantAddressed: true},
		{name: "the people a message names as editor", role: authz.RoleEditor, target: "addressed", wantError: "only an admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := recipientStore(tt.role)
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			form := url.Values{"name": {"new"}, "target": {tt.target}, "priority": {"1"}}
			rec := postFormAs(t, handler, "/admin/routes", form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST = %d, want 303", rec.Code)
			}
			location, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse Location: %v", err)
			}
			if got := location.Query().Get("error"); !strings.Contains(got, tt.wantError) || (tt.wantError == "") != (got == "") {
				t.Fatalf("error = %q, want %q", got, tt.wantError)
			}
			if tt.wantError != "" {
				return
			}

			var saved *models.Route
			for _, route := range st.routes {
				if route.Name == "new" {
					saved = &route
				}
			}
			if saved == nil {
				t.Fatal("route not saved")
			}
			if saved.DestinationID != tt.wantDest || saved.RecipientID != tt.wantRecipient || saved.Addressed != tt.wantAddressed {
				t.Errorf("saved destination=%q recipient=%q, want %q and %q", saved.DestinationID, saved.RecipientID, tt.wantDest, tt.wantRecipient)
			}
		})
	}
}

func TestTheRouteFormOffersOnlyYourOwnChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		role      authz.Role
		query     string
		wantShown []string
		wantGone  []string
	}{
		{
			name: "an editor sees their own chat", role: authz.RoleEditor, query: "/admin",
			wantShown: []string{`value="recipient:mine"`, `value="destination:dest"`},
			wantGone:  []string{`value="recipient:theirs"`},
		},
		{
			name: "an admin sees everyone", role: authz.RoleAdmin, query: "/admin",
			wantShown: []string{`value="recipient:mine"`, `value="recipient:theirs"`},
		},
		{
			// Editing still shows who the route names, even when the save is refused.
			name: "an edited route keeps its person", role: authz.RoleEditor, query: "/admin?edit=routes&id=their-route",
			wantShown: []string{`<option value="recipient:theirs" selected>`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, recipientStore(tt.role), &fakeMessenger{}).Handler
			body := asRole(t, handler, http.MethodGet, tt.query, "").Body.String()
			for _, want := range tt.wantShown {
				if !strings.Contains(body, want) {
					t.Errorf("page does not contain %s", want)
				}
			}
			for _, gone := range tt.wantGone {
				if strings.Contains(body, gone) {
					t.Errorf("page contains %s", gone)
				}
			}
		})
	}
}

// postFormAs posts a same-origin form carrying only the session cookie: basic
// auth is the local administrator and would mask the session's role.
func postFormAs(t *testing.T, handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAnAddressedRouteIsTheAdmins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role        authz.Role
		wantOption  bool
		wantRefused bool
	}{
		{role: authz.RoleAdmin, wantOption: true},
		{role: authz.RoleEditor, wantRefused: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()

			st := recipientStore(tt.role)
			st.routes["pw"] = models.Route{ID: "pw", Name: "Passwords", Addressed: true, LabelSelector: map[string]string{"kind": "password"}}
			cfg := config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
				Bot:     notificationsBotConfig(),
			}
			handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler

			page := asRole(t, handler, http.MethodGet, "/admin", "").Body.String()
			if got := strings.Contains(page, `value="addressed"`); got != tt.wantOption {
				t.Errorf("option offered = %v, want %v", got, tt.wantOption)
			}
			if !strings.Contains(page, "People named in the message") {
				t.Error("the route list does not say where Passwords delivers")
			}

			edit := postFormAs(t, handler, "/admin/routes", url.Values{"id": {"pw"}, "name": {"Renamed"}, "target": {"destination:dest"}, "priority": {"1"}})
			refused := strings.Contains(edit.Header().Get("Location"), "only+an+admin")
			if refused != tt.wantRefused {
				t.Errorf("editing the addressed route refused = %v (%s), want %v", refused, edit.Header().Get("Location"), tt.wantRefused)
			}
			del := postFormAs(t, handler, "/admin/routes/delete", url.Values{"id": {"pw"}})
			if got := strings.Contains(del.Header().Get("Location"), "only+an+admin"); got != tt.wantRefused {
				t.Errorf("deleting the addressed route refused = %v, want %v", got, tt.wantRefused)
			}
		})
	}
}
