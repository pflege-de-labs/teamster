package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func groupsStore(role authz.Role) *fakeStore {
	st := sessionAs(newFakeStore(), role)
	st.groups["g-sre"] = models.Group{ID: "g-sre", Name: "SRE", Description: "site reliability"}
	st.groups["g-oncall"] = models.Group{ID: "g-oncall", Name: "On call"}
	st.members = []models.GroupMember{
		{GroupID: "g-sre", Type: models.MemberUser, ID: "s-bob"},
		{GroupID: "g-sre", Type: models.MemberIdPGroup, ID: "platform"},
	}
	st.users["s-bob"] = models.User{Subject: "s-bob", Source: "oidc", Name: "Bob", IdPGroups: []string{"platform", "devs"}}
	return st
}

func TestGroupsPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		role     authz.Role
		query    string
		want     []string
		unwanted []string
	}{
		{
			name: "the list", role: authz.RoleViewer,
			want:     []string{"SRE", "On call", `href="/admin/groups?id=g-sre"`, `href="/admin/groups"`},
			unwanted: []string{`action="/admin/groups/save"`},
		},
		{
			name: "a group with labelled members and suggestions", role: authz.RoleEditor, query: "?id=g-sre",
			want: []string{`Group::&#34;g-sre&#34;`, "Bob", "s-bob", "platform", `value="idp_group:devs"`, `value="group:g-oncall"`, `data-principal-picker`, `action="/admin/groups/members/add"`, `action="/admin/groups/delete"`},
		},
		{name: "a missing group", role: authz.RoleViewer, query: "?id=nope", want: []string{"No such group."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := newTestServer(t, groupsStore(tt.role), &fakeMessenger{}).Handler
			rec := asRole(t, handler, http.MethodGet, "/admin/groups"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("page lacks %q", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(body, unwanted) {
					t.Errorf("page shows %q", unwanted)
				}
			}
		})
	}

	for _, failOn := range []string{"ListGroups", "ListGroupMembers"} {
		handler := newTestServer(t, groupsStore(authz.RoleAdmin).fail(failOn), &fakeMessenger{}).Handler
		if body := asRole(t, handler, http.MethodGet, "/admin/groups?id=g-sre", "").Body.String(); !strings.Contains(body, "Something went wrong") {
			t.Errorf("a failing %s is not reported", failOn)
		}
	}
	handler := newTestServer(t, groupsStore(authz.RoleAdmin), &fakeMessenger{}).Handler
	if rec := asRole(t, handler, http.MethodPost, "/admin/groups", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/groups = %d, want 405", rec.Code)
	}
}

func TestGroupForms(t *testing.T) {
	t.Parallel()

	st := groupsStore(authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler
	post := func(path string, form url.Values) string {
		t.Helper()
		rec := postFormAs(t, handler, path, form)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST %s = %d", path, rec.Code)
		}
		return rec.Header().Get("Location")
	}

	loc := post("/admin/groups/save", url.Values{"name": {" Platform "}, "description": {"infra"}})
	var created models.Group
	for _, g := range st.groups {
		if g.Name == "Platform" {
			created = g
		}
	}
	if created.ID == "" || created.CreatedBy != "tester" || !strings.Contains(loc, "id="+created.ID) {
		t.Fatalf("created %+v, redirected to %s", created, loc)
	}

	steps := []struct {
		name, path string
		form       url.Values
		wantError  string
	}{
		{"rename", "/admin/groups/save", url.Values{"id": {created.ID}, "name": {"Platform team"}}, ""},
		{"a taken name", "/admin/groups/save", url.Values{"name": {"SRE"}}, "already+exists"},
		{"no name", "/admin/groups/save", url.Values{"name": {"  "}}, "needs+a+name"},
		{"add a group", "/admin/groups/members/add", url.Values{"group_id": {"g-oncall"}, "principal": {"group:g-sre"}}, ""},
		{"a cycle", "/admin/groups/members/add", url.Values{"group_id": {"g-sre"}, "principal": {"group:g-oncall"}}, "contain+itself"},
		{"an unknown type", "/admin/groups/members/add", url.Values{"group_id": {"g-sre"}, "principal": {"role:x"}}, "needs+a+type"},
		{"a missing group", "/admin/groups/members/add", url.Values{"group_id": {"nope"}, "principal": {"user:x"}}, "no+such+group"},
		{"a person not listed", "/admin/groups/members/add", url.Values{"group_id": {"g-oncall"}, "other_type": {"user"}, "other_id": {"new@x"}}, ""},
		{"remove", "/admin/groups/members/remove", url.Values{"group_id": {"g-oncall"}, "principal_type": {"group"}, "principal_id": {"g-sre"}}, ""},
		{"remove again", "/admin/groups/members/remove", url.Values{"group_id": {"g-oncall"}, "principal_type": {"group"}, "principal_id": {"g-sre"}}, "no+such"},
		{"delete", "/admin/groups/delete", url.Values{"id": {created.ID}}, ""},
		{"delete again", "/admin/groups/delete", url.Values{"id": {created.ID}}, "no+such"},
	}
	for _, step := range steps {
		loc := post(step.path, step.form)
		if step.wantError == "" && strings.Contains(loc, "error=") {
			t.Errorf("%s failed: %s", step.name, loc)
		}
		if step.wantError != "" && !strings.Contains(loc, step.wantError) {
			t.Errorf("%s redirected to %s, want an error naming %s", step.name, loc, step.wantError)
		}
	}
	if st.groups[created.ID].Name != "" {
		t.Error("the deleted group is still there")
	}
}

func TestGroupMemberAddByPicker(t *testing.T) {
	t.Parallel()

	st := groupsStore(authz.RoleEditor)
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	if loc := postFormAs(t, h, "/admin/groups/members/add", url.Values{"group_id": {"g-oncall"}, "principal": {"user:s-bob"}}).Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Fatalf("redirected to %s", loc)
	}
	var got bool
	for _, m := range st.members {
		got = got || (m.GroupID == "g-oncall" && m.Type == models.MemberUser && m.ID == "s-bob")
	}
	if !got {
		t.Errorf("member not added: %+v", st.members)
	}

	body := asRole(t, h, http.MethodGet, "/admin/groups?id=g-sre", "").Body.String()
	if !strings.Contains(body, "data-principal-picker") || strings.Contains(body, "<datalist") {
		t.Error("the member form must use the picker and no datalist")
	}
	if !strings.Contains(body, `<input type="hidden" name="principal_type" value="user">`) || !strings.Contains(body, `<input type="hidden" name="principal_id" value="s-bob">`) {
		t.Error("a member's remove form does not post principal_type and principal_id")
	}
	if strings.Contains(body, `value="group:g-sre"`) || strings.Contains(body, `value="role:`) {
		t.Error("the member picker offers the group itself or a role")
	}
}

func TestGroupsAreEditedByEditors(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, groupsStore(authz.RoleViewer), &fakeMessenger{}).Handler
	for _, path := range []string{"/admin/groups/save", "/admin/groups/members/add", "/admin/groups/delete"} {
		if rec := postFormAs(t, handler, path, url.Values{"name": {"x"}}); rec.Code != http.StatusForbidden {
			t.Errorf("viewer POST %s = %d, want 403", path, rec.Code)
		}
	}
}

func TestGroupsAPI(t *testing.T) {
	t.Parallel()

	st := groupsStore(authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodPost, "/api/groups", `{"name":"Platform","id":"ignored"}`)
	var created models.Group
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated || created.ID == "ignored" {
		t.Fatalf("POST /api/groups = %d %s", rec.Code, rec.Body.String())
	}

	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{"list", http.MethodGet, "/api/groups", "", http.StatusOK},
		{"read with members", http.MethodGet, "/api/groups/g-sre", "", http.StatusOK},
		{"read a missing group", http.MethodGet, "/api/groups/nope", "", http.StatusNotFound},
		{"rename", http.MethodPut, "/api/groups/" + created.ID, `{"name":"Platform team"}`, http.StatusOK},
		{"rename onto a taken name", http.MethodPut, "/api/groups/" + created.ID, `{"name":"SRE"}`, http.StatusConflict},
		{"rename with bad JSON", http.MethodPut, "/api/groups/" + created.ID, `{`, http.StatusBadRequest},
		{"create without a name", http.MethodPost, "/api/groups", `{"name":""}`, http.StatusBadRequest},
		{"create with bad JSON", http.MethodPost, "/api/groups", `{`, http.StatusBadRequest},
		{"add a member", http.MethodPost, "/api/groups/" + created.ID + "/members", `{"type":"user","id":"s-bob"}`, http.StatusOK},
		{"add a cycle", http.MethodPost, "/api/groups/g-sre/members", `{"type":"group","id":"g-sre"}`, http.StatusConflict},
		{"add with bad JSON", http.MethodPost, "/api/groups/g-sre/members", `{`, http.StatusBadRequest},
		{"remove a member", http.MethodDelete, "/api/groups/" + created.ID + "/members", `{"type":"user","id":"s-bob"}`, http.StatusOK},
		{"remove a missing member", http.MethodDelete, "/api/groups/" + created.ID + "/members", `{"type":"user","id":"s-bob"}`, http.StatusNotFound},
		{"members by GET", http.MethodGet, "/api/groups/g-sre/members", "", http.StatusMethodNotAllowed},
		{"delete", http.MethodDelete, "/api/groups/" + created.ID, "", http.StatusOK},
		{"delete again", http.MethodDelete, "/api/groups/" + created.ID, "", http.StatusNotFound},
		{"an unknown subpath", http.MethodGet, "/api/groups/g-sre/other", "", http.StatusNotFound},
		{"PATCH", http.MethodPatch, "/api/groups/g-sre", "", http.StatusMethodNotAllowed},
		{"PATCH the list", http.MethodPatch, "/api/groups", "", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		if rec := call(tt.method, tt.path, tt.body); rec.Code != tt.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tt.name, tt.method, tt.path, rec.Code, rec.Body.String(), tt.want)
		}
	}

	var group groupResponse
	if err := json.Unmarshal(call(http.MethodGet, "/api/groups/g-sre", "").Body.Bytes(), &group); err != nil || len(group.Members) != 2 {
		t.Errorf("GET /api/groups/g-sre = %+v, %v", group, err)
	}
	failing := newTestServer(t, groupsStore(authz.RoleEditor).fail("ListGroups"), &fakeMessenger{}).Handler
	if rec := asRole(t, failing, http.MethodGet, "/api/groups", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("failing list = %d, want 500", rec.Code)
	}
}

func TestTheSnapshotLoadsGroupMemberships(t *testing.T) {
	t.Parallel()

	st := groupsStore(authz.RoleViewer)
	model, err := authzSource{st}.Load(t.Context())
	if err != nil || len(model.Members) != 2 {
		t.Fatalf("Load = %+v, %v", model, err)
	}
	if model.Members[1] != (authz.Membership{Group: "g-sre", Kind: "idp_group", ID: "platform"}) {
		t.Errorf("membership = %+v", model.Members[1])
	}
	if _, err := (authzSource{st.fail("ListAllGroupMembers")}).Load(t.Context()); err == nil {
		t.Error("a failing store loaded a model")
	}

	// The session's provider groups reach the principal.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withPrincipal(req.Context(), "s-bob", "Bob", models.ViaSession, []authz.Role{authz.RoleViewer}, []string{"platform"}))
	if p := principalFor(req); p.Subject != "s-bob" || len(p.IdPGroups) != 1 || p.IdPGroups[0] != "platform" {
		t.Errorf("principal = %+v", p)
	}
}

func TestGroupPageSections(t *testing.T) {
	t.Parallel()

	st := groupsStore(authz.RoleAdmin)
	st.templates["t1"] = models.Template{ID: "t1", Name: "One", Body: "{}"}
	st.templates["t2"] = models.Template{ID: "t2", Name: "Two", Body: "{}"}
	st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
	st.groups["g-all"] = models.Group{ID: "g-all", Name: "Everyone"}
	st.members = append(st.members, models.GroupMember{GroupID: "g-all", Type: models.MemberGroup, ID: "g-ops"})
	grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "read", "share")
	grantTo(st, models.PrincipalGroup, "g-all", "Template", "t2", "attach")
	grantTo(st, models.PrincipalGroup, "g-all", "People", authz.PeopleResource.ID, "broadcast")
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	body := asRole(t, handler, http.MethodGet, "/admin/groups?id=g-ops", "").Body.String()
	at := -1
	for _, heading := range []string{">Members<", ">What this group may do<", ">Who may manage this group<", "Technical details"} {
		i := strings.Index(body, heading)
		if i < 0 || i < at {
			t.Fatalf("heading %q missing or out of order", heading)
		}
		at = i
	}
	may := body[strings.Index(body, ">What this group may do<"):strings.Index(body, ">Who may manage this group<")]
	for _, want := range []string{
		"<details", `name="principal" value="group:g-ops"`, "r---s", "group Everyone", `href="/admin/access?principal=group:g-ops"`,
		"anyone, and broadcast to everyone",
	} {
		if !strings.Contains(may, want) {
			t.Errorf("rights section lacks %q", want)
		}
	}
	if strings.Contains(may, `action="/admin/access/messages"`) {
		t.Error("an inherited level has a Remove form")
	}
	if strings.Count(may, "<details") != 1 {
		t.Errorf("want one editable (direct) row, got %d", strings.Count(may, "<details"))
	}
	if strings.Contains(body, `<span class="font-mono text-xs text-slate-500">g-ops</span>`) {
		t.Error("a raw id is shown beside a resolved name")
	}

	st.groups["g-own"] = models.Group{ID: "g-own", Name: "Own"}
	grantTo(st, models.PrincipalGroup, "g-own", "People", authz.PeopleResource.ID, "broadcast")
	grantTo(st, models.PrincipalGroup, "g-own", "Template", "t2", "own")
	own := asRole(t, handler, http.MethodGet, "/admin/groups?id=g-own", "").Body.String()
	if !strings.Contains(own, `action="/admin/access/messages"`) {
		t.Error("a level the group holds has no Remove form")
	}
	if !strings.Contains(own, `name="actions" value="own"`) {
		t.Error("a viewer who may transfer cannot edit a row holding own")
	}

	hub := asRole(t, handler, http.MethodGet, "/admin/access?principal=group:g-ops", "").Body.String()
	if !strings.Contains(hub, `value="group:g-ops"`) || !regexp.MustCompile(`value="group:g-ops"[^>]* selected`).MatchString(hub) {
		t.Error("the hub picker does not preselect the group")
	}
}
