package httpserver

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/cards"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
)

func postForm(t *testing.T, handler http.Handler, path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "pass")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
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
	body := pagesOf(t, handler, "/admin/templates", "/admin/destinations", "/admin/routes")

	// Names, not identifiers: a route stores IDs but the page resolves them.
	for _, want := range []string{
		"Critical card", "Ops channel", "Critical to ops",
		"severity=critical, team=ops", "priority 42",
		`value="tmpl"`,
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
	body := pagesOf(t, handler, "/admin/templates", "/admin/destinations", "/admin/webhooks", "/admin/routes")

	for _, want := range []string{"No templates yet", "No destinations yet", "No routes yet", "none defined yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page does not contain %q", want)
		}
	}
}

func TestAdminPageReportsStoreFailures(t *testing.T) {
	t.Parallel()

	srv, logs := newLoggedTestServer(t, newFakeStore().fail("ListRoutes"), &fakeMessenger{})
	rec := do(t, srv.Handler, http.MethodGet, "/admin/templates", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin = %d, want 200 with the error rendered", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Something went wrong. Reference: "+rec.Header().Get(requestIDHeader)) {
		t.Error("the page does not report the failure with its reference")
	}
	if strings.Contains(body, errStore.Error()) {
		t.Error("the page shows the store's own error")
	}
	if !strings.Contains(logs.String(), errStore.Error()) {
		t.Errorf("log = %q, want the store failure in it", logs.String())
	}
}

func TestAdminPageRejectsNonGet(t *testing.T) {
	t.Parallel()

	rec := postForm(t, newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler, "/admin", url.Values{}, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin = %d, want 405", rec.Code)
	}
}

func TestResourcePageAnswersHead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		want   int
	}{
		{name: "GET", method: http.MethodGet, want: http.StatusOK},
		{name: "HEAD", method: http.MethodHead, want: http.StatusOK},
		{name: "PUT", method: http.MethodPut, want: http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
			rec := do(t, h, tt.method, "/admin/templates", "")
			if rec.Code != tt.want {
				t.Errorf("%s /admin/templates = %d, want %d", tt.method, rec.Code, tt.want)
			}
		})
	}
}

func TestFormsCreateAndUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		form       url.Values
		wantNotice string
		wantTab    string
		check      func(*testing.T, *fakeStore)
	}{
		{
			name:       "create a template",
			path:       "/admin/templates",
			wantTab:    "templates",
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
			wantTab:    "templates",
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
			wantTab:    "destinations",
			form:       url.Values{"name": {"Second"}, "team_id": {"t2"}, "channel_id": {"c2"}},
			wantNotice: "Destination created.",
			check: func(t *testing.T, st *fakeStore) {
				if st.destinations["generated"].TeamID != "t2" {
					t.Errorf("destination not stored: %+v", st.destinations)
				}
			},
		},
		{
			name:    "create a route",
			path:    "/admin/routes",
			wantTab: "routes",
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
			name:       "a route can keep its children from replacing it",
			path:       "/admin/routes",
			wantTab:    "routes",
			form:       url.Values{"name": {"Ops"}, "label_selector": {`{"team":"ops"}`}, "target": {"destination:dest"}, "no_greedy_children": {"true"}},
			wantNotice: "Route created.",
			check: func(t *testing.T, st *fakeStore) {
				if !st.routes["generated"].NoGreedyChildren {
					t.Errorf("route = %+v, want no greedy children", st.routes["generated"])
				}
			},
		},
		{
			name:       "an empty selector is allowed and stays empty",
			path:       "/admin/routes",
			wantTab:    "routes",
			form:       url.Values{"name": {"Default"}, "label_selector": {"  "}, "is_default": {"true"}, "target": {"destination:dest"}},
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
			if parsed, err := url.Parse(location); err != nil || parsed.Path != "/admin/"+tt.wantTab {
				t.Errorf("Location = %q, want the %s page", location, tt.wantTab)
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
			form:    url.Values{"name": {"Card"}, "body": {"{}"}},
			failOn:  "CreateTemplate",
			wantErr: "Something went wrong. Reference: ",
		},
		{
			name:    "the store rejects the delete",
			path:    "/admin/routes/delete",
			form:    url.Values{"id": {"route"}},
			failOn:  "DeleteRoute",
			wantErr: "Something went wrong. Reference: ",
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
			got := location.Query().Get("error")
			if !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", got, tt.wantErr)
			}
			if strings.Contains(got, errStore.Error()) {
				t.Errorf("error = %q shows the store's own error", got)
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
	for _, path := range []string{"/admin/templates/delete", "/admin/destinations/delete", "/admin/routes/global-default"} {
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
			query: "/admin/templates?edit=tmpl",
			wantHTML: []string{
				`<input type="hidden" name="id" value="tmpl">`,
				`value="Critical card"`,
				`{&#34;type&#34;:&#34;AdaptiveCard&#34;}`,
				"Update template",
			},
		},
		{
			name:  "route",
			query: "/admin/routes?edit=route",
			wantHTML: []string{
				`<input type="hidden" name="id" value="route">`,
				`value="Critical to ops"`,
				`<option value="destination:dest" selected>`,
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
			list, _, _ := strings.Cut(tt.query, "?")
			if !strings.Contains(body, `<a href="`+list+`" class="px-3 py-2`) {
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

	body := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/routes?edit=route", "").Body.String()
	if !strings.Contains(body, `name="is_default" value="true" checked`) {
		t.Error("the default flag did not survive into the form")
	}
}

func TestAdminPageOffersAnEditLinkPerRow(t *testing.T) {
	t.Parallel()

	body := pagesOf(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, "/admin/templates", "/admin/destinations", "/admin/routes")

	for _, want := range []string{
		`href="/admin/templates?edit=tmpl"`,
		`href="/admin/routes?edit=route"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list is missing the edit link %q", want)
		}
	}
	// Without a selection every form must still be in create mode, or a visit
	// to /admin would silently update whichever record came first.
	for _, want := range []string{"Save template", "Save route"} {
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
		{name: "unknown id", query: "/admin/templates?edit=missing", wantError: true},
		{name: "section without an id is ignored", query: "/admin/templates"},
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

	if body := do(t, handler, http.MethodGet, "/admin/templates?edit=tmpl", "").Body.String(); !strings.Contains(body, `value="tmpl"`) {
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

// The list is a tree: a child is drawn under the route it refines, with what it
// inherits marked as inherited rather than looking like its own setting.
func TestAdminPageDrawsTheRouteTree(t *testing.T) {
	t.Parallel()

	st := seededUIStore()
	st.destinations["escalation"] = models.Destination{ID: "escalation", Name: "Escalation", TeamID: "team", ChannelID: "esc"}
	st.routes["child"] = models.Route{
		ID: "child", Name: "Payments escalation", ParentID: "route",
		LabelSelector: map[string]string{"team": "payments"}, DestinationID: "escalation",
	}

	body := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/routes", "").Body.String()

	parent := strings.Index(body, "Critical to ops")
	child := strings.Index(body, "Payments escalation")
	if parent < 0 || child < 0 {
		t.Fatal("the list does not show both routes")
	}
	if child < parent {
		t.Error("the child is drawn above the route it refines")
	}
	if !strings.Contains(body, "ml-6") {
		t.Error("the child is not indented, so the tree reads as a flat list")
	}
	if !strings.Contains(body, "Critical card (inherited)") {
		t.Error("the inherited template is not marked as inherited")
	}
	if !strings.Contains(body, "as well as parent") {
		t.Error("the list does not say whether the child delivers as well as its parent")
	}

	// The form has to offer the tree, or a child can only be made through the API.
	if !strings.Contains(body, `name="parent_id"`) || !strings.Contains(body, `name="greedy"`) {
		t.Error("the route form offers no way to nest a route")
	}
}

func TestRouteFormRejectsABrokenTree(t *testing.T) {
	t.Parallel()

	st := seededUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := postForm(t, handler, "/admin/routes", url.Values{
		"name": {"orphan"}, "parent_id": {"gone"}, "label_selector": {`{"team":"payments"}`}, "destination_id": {"dest"},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303 back to the page", rec.Code)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Query().Get("error"); !strings.Contains(got, "does not exist") {
		t.Errorf("error = %q, want it to say the parent is unknown", got)
	}
	if len(st.routes) != 1 {
		t.Errorf("stored %d routes, want the broken one refused", len(st.routes))
	}
}

// The palette inserts fragments that a Go test renders, so what the page offers
// cannot be a card the renderer rejects.
func TestTemplateFormOffersTheCardPalette(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

	for _, want := range []string{
		`class="snippet`, `id="card-starter"`, `id="preview-live"`,
		"FactSet", "AdaptiveCard",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the template form is missing %q", want)
		}
	}

	// Every snippet the package defines is offered, not a copy of some of them.
	// The button carries the fragment, which is the part that has to match; its
	// label is a catalog key and is checked where the catalogs are.
	for _, snippet := range cards.Snippets() {
		if !strings.Contains(body, html.EscapeString(snippet.Body[:40])) {
			t.Errorf("the palette does not offer the %q fragment", snippet.Name)
		}
	}
}

// A viewer sees no form, so it offers nothing to insert into either.
func TestTheCardPaletteIsPartOfTheForm(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleViewer)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

	if strings.Contains(body, `id="card-starter"`) {
		t.Error("a viewer is offered the card palette")
	}
}

func TestAdminRedirects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		noRole   bool
		path     string
		wantCode int
		wantLoc  string
	}{
		{name: "bare admin", path: "/admin", wantCode: http.StatusFound, wantLoc: "/admin/routing"},
		{name: "a share holder without a role", noRole: true, path: "/admin", wantCode: http.StatusFound, wantLoc: "/admin/templates"},
		{name: "tab", path: "/admin?tab=webhooks", wantCode: http.StatusMovedPermanently, wantLoc: "/admin/webhooks"},
		{name: "tab with a notice", path: "/admin?tab=grants&notice=Saved.", wantCode: http.StatusMovedPermanently, wantLoc: "/admin/grants?notice=Saved."},
		{name: "edit", path: "/admin?edit=webhooks&id=abc", wantCode: http.StatusMovedPermanently, wantLoc: "/admin/webhooks?edit=abc"},
		{name: "edit with an error", path: "/admin?edit=routes&id=a%2Fb&error=x", wantCode: http.StatusMovedPermanently, wantLoc: "/admin/routes?edit=a%2Fb&error=x"},
		{name: "unknown tab", path: "/admin?tab=bogus", wantCode: http.StatusFound, wantLoc: "/admin/routing"},
		{name: "unknown edit", path: "/admin?edit=bogus&id=x", wantCode: http.StatusFound, wantLoc: "/admin/routing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(seededUIStore(), authz.RoleAdmin)
			if tt.noRole {
				st = sessionAs(seededUIStore())
				grantTo(st, models.PrincipalUser, "tester", "Template", "tmpl", "read")
			}
			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.wantCode || rec.Header().Get("Location") != tt.wantLoc {
				t.Errorf("GET %s = %d %q, want %d %q", tt.path, rec.Code, rec.Header().Get("Location"), tt.wantCode, tt.wantLoc)
			}
		})
	}
}

func TestHintCardLinksToWebhookPage(t *testing.T) {
	t.Parallel()

	srv := &Server{store: newFakeStore(), cfg: config.Config{Server: config.ServerConfig{ExternalURL: "https://t.example.com"}}}
	msg, err := srv.renderMessage(t.Context(), models.Event{Source: models.SourceTeamsV2, Title: "hi"}, routing.Delivery{WebhookEndpointID: "hook"})
	if err != nil {
		t.Fatalf("renderMessage: %v", err)
	}
	if !strings.Contains(string(msg.Notice), "/admin/webhooks?edit=hook") {
		t.Errorf("notice = %s, want a link to /admin/webhooks?edit=hook", msg.Notice)
	}
}

var resourcePages = []string{"templates", "webhooks", "routes", "grants"}

func TestResourcePages(t *testing.T) {
	t.Parallel()

	for _, page := range resourcePages {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			rec := do(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, http.MethodGet, "/admin/"+page, "")
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /admin/%s = %d, want 200", page, rec.Code)
			}
			for _, other := range resourcePages {
				if has := strings.Contains(body, `id="`+other+`"`); has != (other == page) {
					t.Errorf("section %q present = %v, want %v", other, has, other == page)
				}
			}
			if strings.Contains(body, `role="tablist"`) {
				t.Error("the page still has a tab bar")
			}
			if !strings.Contains(body, "— Teamster</title>") {
				t.Error("page has no resource title")
			}
		})
	}
}

func TestResourcePageEdit(t *testing.T) {
	t.Parallel()

	body := do(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates?edit=tmpl", "").Body.String()
	if !strings.Contains(body, `name="id" value="tmpl"`) {
		t.Error("the form does not edit the template asked for")
	}
}

func TestResourcePageMethods(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, http.MethodPut, "/admin/templates", "")
	allow := rec.Header().Get("Allow")
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Errorf("PUT = %d Allow %q, want 405 allowing GET and POST", rec.Code, allow)
	}
}

func TestFormPostLandsOnResourcePage(t *testing.T) {
	t.Parallel()

	rec := postForm(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler,
		"/admin/templates", url.Values{"name": {"Card"}, "body": {"{}"}}, nil)
	location, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || err != nil || location.Path != "/admin/templates" {
		t.Errorf("POST = %d Location %q, want 303 to /admin/templates", rec.Code, rec.Header().Get("Location"))
	}
}

func TestResourcePageAccess(t *testing.T) {
	t.Parallel()

	type result struct{ templates, other, grants int }
	tests := []struct {
		name  string
		store func() *fakeStore
		want  result
	}{
		{name: "viewer", store: func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleViewer) }, want: result{200, 200, 403}},
		{name: "editor", store: func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleEditor) }, want: result{200, 200, 403}},
		{name: "admin", store: func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) }, want: result{200, 200, 200}},
		{name: "a share only", store: sharedStore, want: result{200, 200, 403}},
		{name: "nothing", store: func() *fakeStore { return sessionAs(seededUIStore()) }, want: result{403, 403, 403}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestServer(t, tt.store(), &fakeMessenger{}).Handler
			for _, page := range resourcePages {
				want := tt.want.other
				switch page {
				case "templates":
					want = tt.want.templates
				case "grants":
					want = tt.want.grants
				}
				if rec := asRole(t, h, http.MethodGet, "/admin/"+page, ""); rec.Code != want {
					t.Errorf("GET /admin/%s = %d, want %d", page, rec.Code, want)
				}
			}
		})
	}
}

func TestResourcePageShowsOnlyShared(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, sharedStore(), &fakeMessenger{}).Handler
	body := asRole(t, h, http.MethodGet, "/admin/templates", "").Body.String()
	if !strings.Contains(body, "Shared card") || strings.Contains(body, "Private card") {
		t.Error("the page should list the shared template and not the other")
	}
}

func TestResourcePagePostsStayClosedToViewers(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, sessionAs(seededUIStore(), authz.RoleViewer), &fakeMessenger{}).Handler
	for _, page := range resourcePages[:4] {
		if rec := asRole(t, h, http.MethodPost, "/admin/"+page, "x=1"); rec.Code != http.StatusForbidden {
			t.Errorf("viewer POST /admin/%s = %d, want 403", page, rec.Code)
		}
	}
}

// pagesOf is the bodies of several pages as one, for assertions about a whole configuration.
func pagesOf(t *testing.T, handler http.Handler, paths ...string) string {
	t.Helper()

	var all strings.Builder
	for _, path := range paths {
		all.WriteString(do(t, handler, http.MethodGet, path, "").Body.String())
	}
	return all.String()
}
