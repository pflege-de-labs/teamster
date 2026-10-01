package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// grantTo seeds a permission and moves the generation, as the store would.
func grantTo(st *fakeStore, kind models.PrincipalType, principal, typ, id string, actions ...string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	pid := "seed-" + string(kind) + "-" + principal + "-" + typ + "-" + id
	st.permissions[pid] = models.Permission{ID: pid, PrincipalType: kind, PrincipalID: principal, ResourceType: typ, ResourceID: id, Actions: actions}
	st.authzGen++
}

// sharedStore is a session with no role, and two templates of which one is shared.
func sharedStore() *fakeStore {
	st := sessionAs(newFakeStore())
	st.templates["t1"] = models.Template{ID: "t1", Name: "Shared card", Body: `{"type":"AdaptiveCard"}`}
	st.templates["t2"] = models.Template{ID: "t2", Name: "Private card", Body: `{"type":"AdaptiveCard"}`}
	grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "read", "update")
	return st
}

func call(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAGrantWithoutARole(t *testing.T) {
	t.Parallel()

	st := sharedStore()
	api, h := capturedServer(t, st)

	page := call(t, h, http.MethodGet, "/admin", "")
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "Shared card") || strings.Contains(body, "Private card") {
		t.Fatalf("GET /admin = %d, want the shared template and not the other", page.Code)
	}
	if !strings.Contains(body, "edit=templates&amp;id=t1") || strings.Contains(body, `action="/admin/templates/delete"`) {
		t.Error("the page should offer editing the shared template and no delete")
	}

	var listed []models.Template
	if err := json.Unmarshal(call(t, h, http.MethodGet, "/api/templates", "").Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != "t1" {
		t.Errorf("GET /api/templates = %+v, %v", listed, err)
	}

	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{"read the shared one", http.MethodGet, "/api/templates/t1", "", http.StatusOK},
		{"update the shared one", http.MethodPut, "/api/templates/t1", `{"name":"Renamed","body":"{}"}`, http.StatusOK},
		{"not delete it", http.MethodDelete, "/api/templates/t1", "", http.StatusForbidden},
		{"not read the other", http.MethodGet, "/api/templates/t2", "", http.StatusForbidden},
		{"not create one", http.MethodPost, "/api/templates", `{"name":"x","body":"{}"}`, http.StatusForbidden},
		{"no route listing beyond what was shared", http.MethodGet, "/api/routes", "", http.StatusOK},
		{"no derived views", http.MethodGet, "/api/routing/graph", "", http.StatusForbidden},
		{"no preview", http.MethodPost, "/api/templates/preview", `{}`, http.StatusForbidden},
	}
	for _, tt := range tests {
		if rec := call(t, h, tt.method, tt.path, tt.body); rec.Code != tt.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tt.name, tt.method, tt.path, rec.Code, rec.Body.String(), tt.want)
		}
	}
	if st.templates["t1"].Name != "Renamed" {
		t.Errorf("t1 = %+v, want it renamed", st.templates["t1"])
	}

	forms := []struct {
		path      string
		form      url.Values
		wantError bool
	}{
		{"/admin/templates", url.Values{"id": {"t1"}, "name": {"Again"}, "body": {"{}"}}, false},
		{"/admin/templates", url.Values{"id": {"t2"}, "name": {"Mine now"}, "body": {"{}"}}, true},
		{"/admin/templates/delete", url.Values{"id": {"t1"}}, true},
	}
	for _, f := range forms {
		loc := postFormAs(t, h, f.path, f.form).Header().Get("Location")
		if strings.Contains(loc, "error=") != f.wantError {
			t.Errorf("POST %s %v redirected to %s", f.path, f.form, loc)
		}
	}
	if st.templates["t2"].Name != "Private card" || st.templates["t1"].Name != "Again" {
		t.Errorf("templates = %+v", st.templates)
	}

	if n := api.unchecked.Load(); n != 0 {
		t.Errorf("%d deferred requests were answered without a record check", n)
	}
}

func TestWithoutAnyGrantNothingChanges(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	st.templates["t1"] = models.Template{ID: "t1", Name: "Card", Body: "{}"}
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	if rec := call(t, h, http.MethodGet, "/admin", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "</html>") {
		t.Errorf("GET /admin with nothing = %d, want the no-access page", rec.Code)
	}
	if rec := call(t, h, http.MethodGet, "/api/templates/t1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("GET a template with nothing = %d, want 403", rec.Code)
	}
}

func TestOwnersShare(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.users["s-bob"] = models.User{Subject: "s-bob", Name: "Bob"}
	st.templates["legacy"] = models.Template{ID: "legacy", Name: "Legacy", Body: "{}"}
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := call(t, h, http.MethodPost, "/api/templates", `{"name":"Mine","body":"{}"}`)
	var created models.Template
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	owners, _ := st.ListPermissionsFor(t.Context(), "Template", created.ID)
	if len(owners) != 1 || owners[0].PrincipalID != "tester" || owners[0].Actions[0] != "own" {
		t.Fatalf("the creator does not own the template: %+v", owners)
	}

	share := func(body string) *httptest.ResponseRecorder {
		return call(t, h, http.MethodPost, "/api/sharing", body)
	}
	tests := []struct {
		name string
		body string
		want int
	}{
		{"the owner shares read", `{"principal_type":"user","principal_id":"s-bob","resource_type":"Template","resource_id":"` + created.ID + `","actions":["read"]}`, http.StatusOK},
		{"the owner passes on ownership", `{"principal_type":"group","principal_id":"g-ops","resource_type":"Template","resource_id":"` + created.ID + `","actions":["own"]}`, http.StatusOK},
		{"an editor may not share what admins own", `{"principal_type":"user","principal_id":"s-bob","resource_type":"Template","resource_id":"legacy","actions":["read"]}`, http.StatusForbidden},
		{"an unknown action", `{"principal_type":"user","principal_id":"s-bob","resource_type":"Template","resource_id":"` + created.ID + `","actions":["administer"]}`, http.StatusBadRequest},
		{"create on a record", `{"principal_type":"user","principal_id":"s-bob","resource_type":"Template","resource_id":"` + created.ID + `","actions":["create"]}`, http.StatusBadRequest},
		{"an unknown principal type", `{"principal_type":"robot","principal_id":"r","resource_type":"Template","resource_id":"` + created.ID + `","actions":["read"]}`, http.StatusBadRequest},
		{"a type with no permissions", `{"principal_type":"user","principal_id":"s-bob","resource_type":"Grant","resource_id":"g","actions":["read"]}`, http.StatusBadRequest},
		{"bad JSON", `{`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		if rec := share(tt.body); rec.Code != tt.want {
			t.Errorf("%s = %d %s, want %d", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}

	var rows []models.Permission
	if err := json.Unmarshal(call(t, h, http.MethodGet, "/api/sharing?type=Template&id="+created.ID, "").Body.Bytes(), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("GET /api/sharing = %+v, %v", rows, err)
	}
	if rec := call(t, h, http.MethodGet, "/api/sharing?type=Grant&id=x", ""); rec.Code != http.StatusForbidden {
		t.Errorf("listing a type with no permissions = %d", rec.Code)
	}
	var bobs string
	for _, p := range rows {
		if p.PrincipalID == "s-bob" {
			bobs = p.ID
		}
	}
	if rec := call(t, h, http.MethodDelete, "/api/sharing/"+bobs, ""); rec.Code != http.StatusOK {
		t.Errorf("revoke = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodDelete, "/api/sharing/"+bobs, ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoke again = %d", rec.Code)
	}
	if rec := call(t, h, http.MethodGet, "/api/sharing/x", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/sharing/x = %d", rec.Code)
	}
	if rec := call(t, h, http.MethodPut, "/api/sharing", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT /api/sharing = %d", rec.Code)
	}

	// Deleting the template takes its permissions along.
	if rec := call(t, h, http.MethodDelete, "/api/templates/"+created.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if left, _ := st.ListPermissionsFor(t.Context(), "Template", created.ID); len(left) != 0 {
		t.Errorf("permissions outlived their template: %+v", left)
	}
}

func TestSharingIsMonotonic(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	st.templates["t1"] = models.Template{ID: "t1", Name: "Card", Body: "{}"}
	grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "read", "share")
	grantTo(st, models.PrincipalUser, "owner", "Template", "t1", "own")
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	tests := []struct {
		name    string
		actions string
		who     string
		want    int
	}{
		{"what the sharer holds", `["read"]`, "s-bob", http.StatusOK},
		{"more than the sharer holds", `["update"]`, "s-carol", http.StatusForbidden},
		{"ownership without transfer", `["own"]`, "s-dave", http.StatusForbidden},
		{"taking ownership away without transfer", `[]`, "owner", http.StatusForbidden},
	}
	for _, tt := range tests {
		body := `{"principal_type":"user","principal_id":"` + tt.who + `","resource_type":"Template","resource_id":"t1","actions":` + tt.actions + `}`
		if rec := call(t, h, http.MethodPost, "/api/sharing", body); rec.Code != tt.want {
			t.Errorf("%s = %d %s, want %d", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}
	if rec := call(t, h, http.MethodDelete, "/api/sharing/seed-user-owner-Template-t1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("revoking an owner without transfer = %d, want 403", rec.Code)
	}
}

func TestSharingForms(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.templates["t1"] = models.Template{ID: "t1", Name: "Card", Body: "{}"}
	grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "own")
	st.groups["g-sre"] = models.Group{ID: "g-sre", Name: "SRE"}
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	page := call(t, h, http.MethodGet, "/admin?edit=templates&id=t1", "").Body.String()
	for _, want := range []string{`id="sharing"`, `action="/admin/sharing/grant"`, `value="g-sre"`, `name="actions" value="own"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the edit page lacks %q", want)
		}
	}

	loc := postFormAs(t, h, "/admin/sharing/grant", url.Values{
		"principal_type": {"group"}, "principal_id": {"g-sre"}, "resource_type": {"Template"}, "resource_id": {"t1"},
		"actions": {"read", "update"}, "return": {"/admin?edit=templates&id=t1"},
	}).Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin?edit=templates") || strings.Contains(loc, "error=") {
		t.Fatalf("grant redirected to %s", loc)
	}
	rows, _ := st.ListPermissionsFor(t.Context(), "Template", "t1")
	var groupRow string
	for _, p := range rows {
		if p.PrincipalID == "g-sre" {
			groupRow = p.ID
		}
	}
	if groupRow == "" {
		t.Fatalf("the group was not granted anything: %+v", rows)
	}

	loc = postFormAs(t, h, "/admin/sharing/revoke", url.Values{"id": {groupRow}, "return": {"https://evil.example/"}}).Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin?") || strings.Contains(loc, "error=") {
		t.Errorf("revoke redirected to %s, want back to /admin", loc)
	}
	loc = postFormAs(t, h, "/admin/sharing/grant", url.Values{"principal_type": {"user"}, "resource_type": {"Template"}, "resource_id": {"t1"}}).Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("a grant without a principal redirected to %s", loc)
	}
	loc = postFormAs(t, h, "/admin/sharing/revoke", url.Values{"id": {"nope"}}).Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("revoking nothing redirected to %s", loc)
	}
}

func TestSafeReturn(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/admin?edit=routes&id=1": "/admin?edit=routes&id=1",
		"/admin/groups?id=g":      "/admin/groups?id=g",
		"https://evil.example/":   "/admin",
		"//evil.example/admin":    "/admin",
		"/elsewhere":              "/admin",
		"/admin\r\nX: y":          "/admin",
		"":                        "/admin",
	}
	for in, want := range tests {
		if got := safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupsCarryGrants(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	st.sessions[testSessionID] = func() models.Session {
		s := st.sessions[testSessionID]
		s.Identity.Groups = []string{"platform"}
		return s
	}()
	st.templates["t1"] = models.Template{ID: "t1", Name: "Card", Body: "{}"}
	st.templates["t2"] = models.Template{ID: "t2", Name: "Other", Body: "{}"}
	st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
	st.groups["g-sre"] = models.Group{ID: "g-sre", Name: "SRE"}
	st.members = []models.GroupMember{
		{GroupID: "g-ops", Type: models.MemberGroup, ID: "g-sre"},
		{GroupID: "g-sre", Type: models.MemberIdPGroup, ID: "platform"},
	}
	grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "read")
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	if rec := call(t, h, http.MethodGet, "/api/templates/t1", ""); rec.Code != http.StatusOK {
		t.Errorf("reading through a provider group in a nested group = %d", rec.Code)
	}
	if rec := call(t, h, http.MethodGet, "/api/templates/t2", ""); rec.Code != http.StatusForbidden {
		t.Errorf("reading what the group was not given = %d", rec.Code)
	}
}

func TestAttachLetsAGrantHolderRoute(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Ops", TeamID: "team", ChannelID: "chan"}
	st.destinations["d2"] = models.Destination{ID: "d2", Name: "Other", TeamID: "team", ChannelID: "other"}
	st.templates["t1"] = models.Template{ID: "t1", Name: "Card", Body: "{}"}
	grantTo(st, models.PrincipalUser, "tester", "Route", "*", "create")
	grantTo(st, models.PrincipalUser, "tester", "Destination", "d1", "attach", "read", "update")
	grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "attach")
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{"into the attached destination", http.MethodPost, "/api/routes", `{"name":"mine","destination_id":"d1","template_id":"t1","is_default":true}`, http.StatusCreated},
		{"into another", http.MethodPost, "/api/routes", `{"name":"theirs","destination_id":"d2","is_default":true}`, http.StatusForbidden},
		{"with no template", http.MethodPost, "/api/routes", `{"name":"plain","destination_id":"d1","is_default":true}`, http.StatusCreated},
		{"rename the destination in place", http.MethodPut, "/api/destinations/d1", `{"name":"Ops renamed","team_id":"team","channel_id":"chan"}`, http.StatusOK},
		{"move it elsewhere", http.MethodPut, "/api/destinations/d1", `{"name":"Ops","team_id":"team","channel_id":"other"}`, http.StatusForbidden},
	}
	for _, tt := range tests {
		if rec := call(t, h, tt.method, tt.path, tt.body); rec.Code != tt.want {
			t.Errorf("%s = %d %s, want %d", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}
	var listed []models.Destination
	if err := json.Unmarshal(call(t, h, http.MethodGet, "/api/destinations", "").Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != "d1" {
		t.Errorf("GET /api/destinations = %+v, %v", listed, err)
	}
}

func TestTheWitnessCatchesAnUncheckedHandler(t *testing.T) {
	t.Parallel()

	s := &Server{}
	quiet := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	careful := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { markChecked(r) })
	refusing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })

	for _, h := range []http.Handler{careful, refusing} {
		s.serveDeferred(h, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/templates/t", nil))
	}
	if n := s.unchecked.Load(); n != 0 {
		t.Fatalf("unchecked = %d after handlers that checked or refused", n)
	}
	s.serveDeferred(quiet, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/templates/t", nil))
	if n := s.unchecked.Load(); n != 1 {
		t.Errorf("unchecked = %d, want the quiet handler counted", n)
	}
}

func TestRecordPaths(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"/admin":                         true,
		"/admin/templates":               true,
		"/api/templates/t1":              true,
		"/api/sharing/p1":                true,
		"/api/templates/preview":         false,
		"/api/templates/source-defaults": false,
		"/api/routes/global-default":     false,
		"/api/destinations/d1/default":   false,
		"/admin/routing":                 false,
		"/api/routing/graph":             false,
		"/api/config/export":             false,
	}
	for path, want := range tests {
		if got := recordPath(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Errorf("recordPath(%s) = %v, want %v", path, got, want)
		}
	}
}

// Every endpoint a grant holder reaches checks its record: none trips the witness.
func TestDeferredEndpointsAllCheck(t *testing.T) {
	t.Parallel()

	st := sharedStore()
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Ops", TeamID: "team", ChannelID: "chan"}
	st.routes["r1"] = models.Route{ID: "r1", Name: "r", DestinationID: "d1", IsDefault: true}
	st.webhooks["w1"] = models.WebhookEndpoint{ID: "w1", TeamSlug: "a", ChannelSlug: "b", DestinationID: "d1"}
	st.groups["g1"] = models.Group{ID: "g1", Name: "G"}
	for _, typ := range []string{"Destination", "Route", "WebhookEndpoint", "Group"} {
		id := map[string]string{"Destination": "d1", "Route": "r1", "WebhookEndpoint": "w1", "Group": "g1"}[typ]
		grantTo(st, models.PrincipalUser, "tester", typ, id, "read")
	}
	api, h := capturedServer(t, st)

	gets := []string{
		"/admin", "/admin?edit=templates&id=t1", "/admin?edit=routes&id=r1", "/admin?edit=webhooks&id=w1", "/admin?edit=destinations&id=d1",
		"/api/templates", "/api/templates/t1", "/api/destinations", "/api/destinations/d1", "/api/routes", "/api/routes/r1",
		"/api/webhooks", "/api/webhooks/w1", "/api/groups", "/api/groups/g1", "/admin/groups", "/admin/groups?id=g1",
		"/api/sharing?type=Template&id=t1",
	}
	for _, path := range gets {
		if rec := call(t, h, http.MethodGet, path, ""); rec.Code >= 400 {
			t.Errorf("GET %s = %d", path, rec.Code)
		}
	}
	posts := map[string]url.Values{
		"/admin/templates":             {"id": {"t2"}, "name": {"x"}, "body": {"{}"}},
		"/admin/templates/delete":      {"id": {"t1"}},
		"/admin/destinations":          {"id": {"d1"}, "name": {"x"}, "team_id": {"team"}, "channel_id": {"chan"}},
		"/admin/destinations/delete":   {"id": {"d1"}},
		"/admin/routes":                {"id": {"r1"}, "name": {"x"}},
		"/admin/routes/delete":         {"id": {"r1"}},
		"/admin/webhooks":              {"id": {"w1"}, "team_slug": {"a"}, "channel_slug": {"b"}, "destination_id": {"d1"}},
		"/admin/webhooks/rotate":       {"id": {"w1"}},
		"/admin/webhooks/delete":       {"id": {"w1"}},
		"/admin/groups/save":           {"id": {"g1"}, "name": {"x"}},
		"/admin/groups/delete":         {"id": {"g1"}},
		"/admin/groups/members/add":    {"group_id": {"g1"}, "type": {"user"}, "member": {"x"}},
		"/admin/groups/members/remove": {"group_id": {"g1"}, "type": {"user"}, "member": {"x"}},
		"/admin/sharing/grant":         {"principal_type": {"user"}, "principal_id": {"x"}, "resource_type": {"Template"}, "resource_id": {"t1"}, "actions": {"read"}},
		"/admin/sharing/revoke":        {"id": {"seed-user-tester-Template-t1"}},
	}
	for path, form := range posts {
		postFormAs(t, h, path, form)
	}
	if n := api.unchecked.Load(); n != 0 {
		t.Errorf("%d deferred requests were answered without a record check", n)
	}
}

// capturedServer is newTestServer that also hands back the Server behind it.
func capturedServer(t *testing.T, st *fakeStore) (*Server, http.Handler) {
	t.Helper()
	var api *Server
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	msg := &fakeMessenger{}
	srv, err := NewServer(quietLog, cfg, st, msg, nil, msg, metrics.Disabled(), nil, func(s *Server) { api = s })
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return api, srv.Handler
}
