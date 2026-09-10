package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func postForm(t *testing.T, handler http.Handler, path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "pass")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func seededUIStore() *fakeStore {
	st := newFakeStore()
	st.templates["tmpl"] = models.Template{
		ID: "tmpl", Name: "Critical card", Body: `{"type":"AdaptiveCard"}`,
		UpdatedAt: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
	}
	st.destinations["dest"] = models.Destination{ID: "dest", Name: "Ops channel", TeamID: "team", ChannelID: "chan"}
	st.routes["route"] = models.Route{
		ID: "route", Name: "Critical to ops", TemplateID: "tmpl", DestinationID: "dest",
		LabelSelector: map[string]string{"severity": "critical", "team": "ops"}, Priority: 42,
	}
	return st
}

func TestAdminPageRendersTheStoredConfiguration(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler
	body := do(t, handler, http.MethodGet, "/admin", "").Body.String()

	// Names, not identifiers: a route stores IDs but the page resolves them.
	for _, want := range []string{
		"Critical card", "Ops channel", "Critical to ops",
		"severity=critical, team=ops", "priority 42",
		`value="dest"`, `value="tmpl"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page does not contain %q", want)
		}
	}
	if strings.Contains(body, `placeholder="destination-id"`) {
		t.Error("the destination is still a free-text id field, want a select")
	}
}

func TestAdminPageRendersEmptyStates(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	body := do(t, handler, http.MethodGet, "/admin", "").Body.String()

	for _, want := range []string{"No templates yet", "No destinations yet", "No routes yet", "none defined yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page does not contain %q", want)
		}
	}
}

func TestAdminPageReportsStoreFailures(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore().fail("ListRoutes"), &fakeMessenger{}).Handler
	rec := do(t, handler, http.MethodGet, "/admin", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin = %d, want 200 with the error rendered", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), errStore.Error()) {
		t.Error("the page hides the store failure")
	}
}

func TestAdminPageRejectsNonGet(t *testing.T) {
	t.Parallel()

	rec := postForm(t, newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler, "/admin", url.Values{}, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin = %d, want 405", rec.Code)
	}
}

func TestFormsCreateAndUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		form       url.Values
		wantNotice string
		check      func(*testing.T, *fakeStore)
	}{
		{
			name:       "create a template",
			path:       "/admin/templates",
			form:       url.Values{"name": {"New card"}, "body": {"{}"}},
			wantNotice: "Template created.",
			check: func(t *testing.T, st *fakeStore) {
				if st.templates["generated"].Name != "New card" {
					t.Errorf("template not stored: %+v", st.templates)
				}
			},
		},
		{
			name:       "update a template",
			path:       "/admin/templates",
			form:       url.Values{"id": {"tmpl"}, "name": {"Renamed"}, "body": {"{}"}},
			wantNotice: "Template updated.",
			check: func(t *testing.T, st *fakeStore) {
				if st.templates["tmpl"].Name != "Renamed" {
					t.Errorf("template not updated: %+v", st.templates["tmpl"])
				}
			},
		},
		{
			name:       "create a destination",
			path:       "/admin/destinations",
			form:       url.Values{"name": {"Second"}, "team_id": {"t2"}, "channel_id": {"c2"}},
			wantNotice: "Destination created.",
			check: func(t *testing.T, st *fakeStore) {
				if st.destinations["generated"].TeamID != "t2" {
					t.Errorf("destination not stored: %+v", st.destinations)
				}
			},
		},
		{
			name: "create a route",
			path: "/admin/routes",
			form: url.Values{
				"name": {"Warnings"}, "label_selector": {`{"severity":"warning"}`},
				"destination_id": {"dest"}, "template_id": {"tmpl"},
				"priority": {"7"}, "is_default": {"true"},
			},
			wantNotice: "Route created.",
			check: func(t *testing.T, st *fakeStore) {
				got := st.routes["generated"]
				if got.Priority != 7 || !got.IsDefault || got.LabelSelector["severity"] != "warning" {
					t.Errorf("route not stored as submitted: %+v", got)
				}
			},
		},
		{
			name:       "an empty selector is allowed and stays empty",
			path:       "/admin/routes",
			form:       url.Values{"name": {"Default"}, "label_selector": {"  "}, "is_default": {"true"}},
			wantNotice: "Route created.",
			check: func(t *testing.T, st *fakeStore) {
				if len(st.routes["generated"].LabelSelector) != 0 {
					t.Errorf("selector = %v, want empty", st.routes["generated"].LabelSelector)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seededUIStore()
			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.path, tt.form, nil)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s = %d, want 303 (body %s)", tt.path, rec.Code, rec.Body.String())
			}
			location := rec.Header().Get("Location")
			if !strings.Contains(location, url.QueryEscape(tt.wantNotice)) {
				t.Errorf("Location = %q, want the notice %q", location, tt.wantNotice)
			}
			tt.check(t, st)
		})
	}
}

func TestFormsDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		id     string
		remain func(*fakeStore) int
	}{
		{name: "template", path: "/admin/templates/delete", id: "tmpl", remain: func(s *fakeStore) int { return len(s.templates) }},
		{name: "destination", path: "/admin/destinations/delete", id: "dest", remain: func(s *fakeStore) int { return len(s.destinations) }},
		{name: "route", path: "/admin/routes/delete", id: "route", remain: func(s *fakeStore) int { return len(s.routes) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seededUIStore()
			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.path, url.Values{"id": {tt.id}}, nil)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s = %d, want 303", tt.path, rec.Code)
			}
			if tt.remain(st) != 0 {
				t.Errorf("%s was not deleted", tt.name)
			}
		})
	}
}

func TestFormsReportErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		form    url.Values
		failOn  string
		wantErr string
	}{
		{
			name:    "selector is not JSON",
			path:    "/admin/routes",
			form:    url.Values{"name": {"Broken"}, "label_selector": {"not json"}},
			wantErr: "invalid character",
		},
		{
			name:    "priority is not a number",
			path:    "/admin/routes",
			form:    url.Values{"name": {"Broken"}, "priority": {"soon"}},
			wantErr: "invalid syntax",
		},
		{
			name:    "the store rejects the template",
			path:    "/admin/templates",
			form:    url.Values{"name": {"Card"}},
			failOn:  "CreateTemplate",
			wantErr: errStore.Error(),
		},
		{
			name:    "the store rejects the delete",
			path:    "/admin/routes/delete",
			form:    url.Values{"id": {"route"}},
			failOn:  "DeleteRoute",
			wantErr: errStore.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seededUIStore()
			if tt.failOn != "" {
				st.fail(tt.failOn)
			}

			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.path, tt.form, nil)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s = %d, want 303 carrying the error", tt.path, rec.Code)
			}

			location, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse Location: %v", err)
			}
			if got := location.Query().Get("error"); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", got, tt.wantErr)
			}
		})
	}
}

// Basic auth credentials ride along on a cross-site form post, so the origin
// check is the only thing standing between a hostile page and the store.
func TestFormsRejectCrossOriginPosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{name: "same-origin fetch metadata", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, wantStatus: http.StatusSeeOther},
		{name: "browser-initiated navigation", headers: map[string]string{"Sec-Fetch-Site": "none"}, wantStatus: http.StatusSeeOther},
		{name: "cross-site fetch metadata", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, wantStatus: http.StatusForbidden},
		{name: "same-site is still another origin", headers: map[string]string{"Sec-Fetch-Site": "same-site"}, wantStatus: http.StatusForbidden},
		{name: "foreign origin header", headers: map[string]string{"Origin": "https://evil.example"}, wantStatus: http.StatusForbidden},
		{name: "matching origin header", headers: map[string]string{"Origin": "http://example.com"}, wantStatus: http.StatusSeeOther},
		{name: "unparseable origin", headers: map[string]string{"Origin": "://"}, wantStatus: http.StatusForbidden},
		{name: "no metadata at all, as a script would send", wantStatus: http.StatusSeeOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seededUIStore()
			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler,
				"/admin/templates", url.Values{"name": {"Card"}, "body": {"{}"}}, tt.headers)

			if rec.Code != tt.wantStatus {
				t.Errorf("POST = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusForbidden && len(st.templates) != 1 {
				t.Error("a rejected post still changed the store")
			}
		})
	}
}

func TestFormsRejectNonPost(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	for _, path := range []string{"/admin/templates", "/admin/destinations/delete", "/admin/routes"} {
		if rec := do(t, handler, http.MethodGet, path, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 405", path, rec.Code)
		}
	}
}

func TestAdminPagePrefillsTheFormBeingEdited(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		query    string
		wantHTML []string
	}{
		{
			name:  "template",
			query: "/admin?edit=templates&id=tmpl",
			wantHTML: []string{
				`<input type="hidden" name="id" value="tmpl">`,
				`value="Critical card"`,
				`{&#34;type&#34;:&#34;AdaptiveCard&#34;}`,
				"Update template",
			},
		},
		{
			name:  "destination",
			query: "/admin?edit=destinations&id=dest",
			wantHTML: []string{
				`<input type="hidden" name="id" value="dest">`,
				`value="Ops channel"`,
				`value="team"`,
				`value="chan"`,
				"Update destination",
			},
		},
		{
			name:  "route",
			query: "/admin?edit=routes&id=route",
			wantHTML: []string{
				`<input type="hidden" name="id" value="route">`,
				`value="Critical to ops"`,
				`<option value="dest" selected>`,
				`<option value="tmpl" selected>`,
				`value="42"`,
				"Update route",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler
			body := do(t, handler, http.MethodGet, tt.query, "").Body.String()

			for _, want := range tt.wantHTML {
				if !strings.Contains(body, want) {
					t.Errorf("prefilled page does not contain %q", want)
				}
			}
			if !strings.Contains(body, `href="/admin"`) {
				t.Error("no cancel link back to the unfiltered page")
			}
		})
	}
}

func TestAdminPageChecksTheDefaultRouteBox(t *testing.T) {
	t.Parallel()

	st := seededUIStore()
	route := st.routes["route"]
	route.IsDefault = true
	st.routes["route"] = route

	body := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin?edit=routes&id=route", "").Body.String()
	if !strings.Contains(body, `name="is_default" value="true" checked`) {
		t.Error("the default flag did not survive into the form")
	}
}

func TestAdminPageOffersAnEditLinkPerRow(t *testing.T) {
	t.Parallel()

	body := do(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()

	for _, want := range []string{
		`href="/admin?edit=templates&amp;id=tmpl#templates"`,
		`href="/admin?edit=destinations&amp;id=dest#destinations"`,
		`href="/admin?edit=routes&amp;id=route#routes"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list is missing the edit link %q", want)
		}
	}
	// Without a selection every form must still be in create mode, or a visit
	// to /admin would silently update whichever record came first.
	for _, want := range []string{"Save template", "Save destination", "Save route"} {
		if !strings.Contains(body, want) {
			t.Errorf("form is not in create mode: %q missing", want)
		}
	}
}

func TestAdminPageHandlesBadEditRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		wantError bool
	}{
		{name: "unknown id", query: "/admin?edit=templates&id=missing", wantError: true},
		{name: "unknown section is ignored", query: "/admin?edit=nonsense&id=tmpl"},
		{name: "section without an id is ignored", query: "/admin?edit=templates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, http.MethodGet, tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.query, rec.Code)
			}

			body := rec.Body.String()
			if got := strings.Contains(body, "not found"); got != tt.wantError {
				t.Errorf("error shown = %v, want %v", got, tt.wantError)
			}
			// The delete buttons carry ids of their own, so the submit label is
			// what distinguishes a prefilled form from an empty one.
			if strings.Contains(body, "Update template") {
				t.Error("a form was prefilled for a request that selected nothing")
			}
		})
	}
}

// The point of the whole feature: what the edit link offers must come back as
// an update, not a second record.
func TestEditRoundTripUpdatesInPlace(t *testing.T) {
	t.Parallel()

	st := seededUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	if body := do(t, handler, http.MethodGet, "/admin?edit=templates&id=tmpl", "").Body.String(); !strings.Contains(body, `value="tmpl"`) {
		t.Fatal("the edit page did not offer the id back")
	}

	rec := postForm(t, handler, "/admin/templates", url.Values{
		"id": {"tmpl"}, "name": {"Renamed card"}, "body": {"{}"},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303", rec.Code)
	}

	if len(st.templates) != 1 {
		t.Errorf("store holds %d templates, want the original updated in place", len(st.templates))
	}
	if st.templates["tmpl"].Name != "Renamed card" {
		t.Errorf("template not updated: %+v", st.templates["tmpl"])
	}
}
