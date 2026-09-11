package httpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// scopedStore is an editor limited to one Team, with a destination inside it
// and one outside.
func scopedStore(t *testing.T, role authz.Role) *fakeStore {
	t.Helper()

	st := sessionAs(newFakeStore(), role)
	st.grants["g1"] = models.Grant{ID: "g1", Role: string(role), TeamID: "platform"}
	st.destinations["inside"] = models.Destination{ID: "inside", Name: "Platform alerts", TeamID: "platform", ChannelID: "alerts"}
	st.destinations["outside"] = models.Destination{ID: "outside", Name: "Payments alerts", TeamID: "payments", ChannelID: "alerts"}
	return st
}

func TestGrantsLimitTheDestinationsThatCanBeCreated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "inside the granted Team",
			body:       `{"name":"ok","team_id":"platform","channel_id":"incidents"}`,
			wantStatus: http.StatusCreated,
		},
		{
			// The picker would not have offered it; typing the id must not be a
			// way round that.
			name:       "outside it",
			body:       `{"name":"no","team_id":"payments","channel_id":"alerts"}`,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := scopedStore(t, authz.RoleEditor)
			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/destinations", tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("POST = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// Moving a destination into an ungranted Team is the same escape as creating
// one there.
func TestGrantsLimitDestinationUpdates(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor)
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPut, "/api/destinations/inside",
		`{"name":"moved","team_id":"payments","channel_id":"alerts"}`)

	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if st.destinations["inside"].TeamID != "platform" {
		t.Errorf("destination = %+v, want it left where it was", st.destinations["inside"])
	}
}

// The form path has to refuse what the API path refuses; it is the one an
// operator actually uses.
func TestGrantsLimitTheDestinationForm(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	form := url.Values{"name": {"sneaky"}, "team_id": {"payments"}, "channel_id": {"alerts"}}
	rec := postForm(t, handler, "/admin/destinations", form, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303 carrying the error", rec.Code)
	}

	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Query().Get("error"); !strings.Contains(got, "not granted") {
		t.Errorf("error = %q, want it to say the Team is not granted", got)
	}
	if len(st.destinations) != 2 {
		t.Errorf("stored %d destinations, want the refused one absent", len(st.destinations))
	}
}

func TestGrantsHideWhatIsOutOfScope(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleViewer)
	st.grants["g1"] = models.Grant{ID: "g1", Role: "viewer", TeamID: "platform"}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := asRole(t, handler, http.MethodGet, "/api/destinations", "")
	var listed []models.Destination
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode destinations: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "inside" {
		t.Errorf("destinations = %+v, want only the one in the granted Team", listed)
	}

	page := asRole(t, handler, http.MethodGet, "/admin", "").Body.String()
	if strings.Contains(page, "Payments alerts") {
		t.Error("the page shows a destination outside the grants")
	}
	if !strings.Contains(page, "Platform alerts") {
		t.Error("the page hides a destination inside the grants")
	}
}

// The pickers offer what the grants reach, so an operator is not shown Teams
// they will be refused when they save.
func TestPickersOfferOnlyGrantedTeamsAndChannels(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor)
	msg := &fakeMessenger{
		teams: []graph.Team{{ID: "platform", Name: "Platform"}, {ID: "payments", Name: "Payments"}},
		channels: map[string][]graph.Channel{
			"platform": {{ID: "alerts", Name: "Alerts"}, {ID: "incidents", Name: "Incidents"}},
		},
	}
	handler := newTestServer(t, st, msg).Handler

	teams := asRole(t, handler, http.MethodGet, "/api/graph/teams", "").Body.String()
	if !strings.Contains(teams, "Platform") || strings.Contains(teams, "Payments") {
		t.Errorf("teams = %s, want only the granted one", teams)
	}

	// A Team grant reaches every channel in it.
	channels := asRole(t, handler, http.MethodGet, "/api/graph/teams/platform/channels", "").Body.String()
	if !strings.Contains(channels, "Alerts") || !strings.Contains(channels, "Incidents") {
		t.Errorf("channels = %s, want all of the granted Team's", channels)
	}
}

// A channel grant is narrower than its Team: the Team shows up in the picker,
// the other channels of it do not.
func TestAChannelGrantOffersOnlyThatChannel(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.grants["g1"] = models.Grant{ID: "g1", Role: "editor", TeamID: "platform", ChannelID: "alerts"}
	msg := &fakeMessenger{
		teams: []graph.Team{{ID: "platform", Name: "Platform"}},
		channels: map[string][]graph.Channel{
			"platform": {{ID: "alerts", Name: "Alerts"}, {ID: "incidents", Name: "Incidents"}},
		},
	}
	handler := newTestServer(t, st, msg).Handler

	channels := asRole(t, handler, http.MethodGet, "/api/graph/teams/platform/channels", "").Body.String()
	if !strings.Contains(channels, "Alerts") || strings.Contains(channels, "Incidents") {
		t.Errorf("channels = %s, want only the granted channel", channels)
	}
}

// Deciding who may deliver where is the admin's.
func TestOnlyAnAdminManagesGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role       authz.Role
		wantStatus int
	}{
		{role: authz.RoleAdmin, wantStatus: http.StatusCreated},
		{role: authz.RoleEditor, wantStatus: http.StatusForbidden},
		{role: authz.RoleViewer, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), tt.role)
			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/grants",
				`{"role":"editor","team_id":"platform"}`)
			if rec.Code != tt.wantStatus {
				t.Errorf("POST /api/grants as %s = %d, want %d (%s)", tt.role, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestGrantValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "a Team for a role", body: `{"role":"editor","team_id":"platform"}`, wantStatus: http.StatusCreated},
		{name: "a channel of it", body: `{"role":"editor","team_id":"platform","channel_id":"alerts"}`, wantStatus: http.StatusCreated},
		{name: "without a role", body: `{"team_id":"platform"}`, wantStatus: http.StatusBadRequest},
		{name: "without a Team", body: `{"role":"editor"}`, wantStatus: http.StatusBadRequest},
		{name: "not JSON", body: `{`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), authz.RoleAdmin)
			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/grants", tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("POST = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestGrantsPanelIsAdminOnly(t *testing.T) {
	t.Parallel()

	admin := sessionAs(seededUIStore(), authz.RoleAdmin)
	admin.grants["g1"] = models.Grant{ID: "g1", Role: "editor", TeamID: "platform"}
	page := asRole(t, newTestServer(t, admin, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	if !strings.Contains(page, "Delivery permissions") || !strings.Contains(page, "team platform") {
		t.Error("an admin is not shown the grants")
	}

	editor := sessionAs(seededUIStore(), authz.RoleEditor)
	page = asRole(t, newTestServer(t, editor, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	if strings.Contains(page, "Delivery permissions") {
		t.Error("an editor is shown the grants panel")
	}
}

// An unreachable store must refuse rather than fall open: a scope that cannot
// be read is not a scope that permits everything.
func TestAFailedGrantLookupRefuses(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor).fail("ListGrants")
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/destinations",
		`{"name":"x","team_id":"platform","channel_id":"alerts"}`)

	if rec.Code == http.StatusCreated {
		t.Errorf("POST = %d, want the store failure to refuse the write", rec.Code)
	}
}

// A route is how an alert reaches a channel, so pointing one at a destination
// outside the grants has to be refused the same way creating that destination
// would be.
func TestGrantsLimitTheDestinationARouteMayUse(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor)
	st.templates["tmpl"] = models.Template{ID: "tmpl", Name: "Card", Body: "{}"}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	refused := asRole(t, handler, http.MethodPost, "/api/routes",
		`{"name":"sneaky","destination_id":"outside","template_id":"tmpl","is_default":true}`)
	if refused.Code != http.StatusForbidden {
		t.Errorf("POST route to an ungranted destination = %d, want 403 (%s)", refused.Code, refused.Body.String())
	}

	allowed := asRole(t, handler, http.MethodPost, "/api/routes",
		`{"name":"fine","destination_id":"inside","template_id":"tmpl","is_default":true}`)
	if allowed.Code != http.StatusCreated {
		t.Errorf("POST route to a granted destination = %d, want 201 (%s)", allowed.Code, allowed.Body.String())
	}

	// Moving an existing route out of scope is the same escape.
	st.routes["known"] = models.Route{ID: "known", Name: "known", DestinationID: "inside", TemplateID: "tmpl"}
	moved := asRole(t, handler, http.MethodPut, "/api/routes/known",
		`{"name":"known","destination_id":"outside","template_id":"tmpl"}`)
	if moved.Code != http.StatusForbidden {
		t.Errorf("PUT route onto an ungranted destination = %d, want 403", moved.Code)
	}
	if st.routes["known"].DestinationID != "inside" {
		t.Errorf("route = %+v, want it left where it was", st.routes["known"])
	}
}

// A route may point at a destination that no longer exists — the graph draws
// that as broken rather than refusing to save it.
func TestARouteMayPointAtAMissingDestination(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor)
	st.templates["tmpl"] = models.Template{ID: "tmpl", Name: "Card", Body: "{}"}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/routes",
		`{"name":"dangling","destination_id":"gone","template_id":"tmpl","is_default":true}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("POST = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
}

// Team and channel ids are typed by hand, so one carrying the separator that
// joins them in an entity id must not resolve to a Team that was granted.
func TestACraftedIDCannotBorrowAGrant(t *testing.T) {
	t.Parallel()

	st := scopedStore(t, authz.RoleEditor)
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/destinations",
		"{\"name\":\"crafted\",\"team_id\":\"platform\\u001fpayments\",\"channel_id\":\"alerts\"}")

	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with a crafted Team id = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}
