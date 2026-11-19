package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestWebhookLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level string
		want  []string
	}{
		{"none", nil},
		{"alertmanager", []string{"alertmanager use"}},
		{"universal", []string{"universal use"}},
		{"all", []string{"* use"}},
		{"admin", []string{"* use administer"}},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(newFakeStore(), authz.RoleAdmin)
			st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
			// Whatever was there before is replaced.
			grantTo(st, models.PrincipalGroup, "g-ops", "Webhook", "universal", "use")
			h := newTestServer(t, st, &fakeMessenger{}).Handler

			loc := postFormAs(t, h, "/admin/access/webhooks", url.Values{
				"principal": {"group:g-ops"}, "level": {tt.level},
			}).Header().Get("Location")
			if strings.Contains(loc, "error=") {
				t.Fatalf("set level redirected to %s", loc)
			}
			var got []string
			for _, p := range st.permissions {
				if p.ResourceType == "Webhook" {
					got = append(got, p.ResourceID+" "+strings.Join(p.Actions, " "))
				}
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("rows = %v, want %v", got, tt.want)
			}
		})
	}

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	for _, form := range []url.Values{
		{"principal_type": {"group"}, "principal_id": {"g"}, "level": {"everything"}},
		{"principal_type": {"robot"}, "principal_id": {"g"}, "level": {"all"}},
		{"principal_type": {"user"}, "principal_id": {" "}, "level": {"all"}},
	} {
		if loc := postFormAs(t, h, "/admin/access/webhooks", form).Header().Get("Location"); !strings.Contains(loc, "error=") {
			t.Errorf("%v was accepted", form)
		}
	}

	apiTests := []struct {
		body string
		want int
	}{
		{`{"principal_type":"role","principal_id":"viewer","level":"alertmanager"}`, http.StatusOK},
		{`{"principal_type":"role","principal_id":"viewer","level":"x"}`, http.StatusBadRequest},
		{`{`, http.StatusBadRequest},
	}
	for _, tt := range apiTests {
		if rec := call(t, h, http.MethodPut, "/api/access/webhooks", tt.body); rec.Code != tt.want {
			t.Errorf("PUT %s = %d, want %d", tt.body, rec.Code, tt.want)
		}
	}
	if rec := call(t, h, http.MethodGet, "/api/access/webhooks", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", rec.Code)
	}
}

// TestLevelsByPicker checks the level forms read the picker's "type:id" and its
// "someone not listed" fields.
func TestLevelsByPicker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, path string
		form       url.Values
		wantType   models.PrincipalType
		wantID     string
	}{
		{"webhook other user", "/admin/access/webhooks", url.Values{"other_type": {"user"}, "other_id": {"new@x"}, "level": {"all"}}, models.PrincipalUser, "new@x"},
		{"message picked idp group", "/admin/access/messages", url.Values{"principal": {"idp_group:office"}, "level": {"anyone"}}, models.PrincipalIdPGroup, "office"},
		{"message other role", "/admin/access/messages", url.Values{"other_type": {"role"}, "other_id": {"ops"}, "level": {"anyone"}}, models.PrincipalRole, "ops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(newFakeStore(), authz.RoleAdmin)
			h := newTestServer(t, st, &fakeMessenger{}).Handler
			if loc := postFormAs(t, h, tt.path, tt.form).Header().Get("Location"); strings.Contains(loc, "error=") {
				t.Fatalf("redirected to %s", loc)
			}
			found := false
			for _, p := range st.permissions {
				found = found || (p.PrincipalType == tt.wantType && p.PrincipalID == tt.wantID)
			}
			if !found {
				t.Errorf("no permission for %s:%s in %v", tt.wantType, tt.wantID, st.permissions)
			}
		})
	}
}

func TestLevelFormsUsePickers(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, sessionAs(newFakeStore(), authz.RoleAdmin), &fakeMessenger{}).Handler
	body := call(t, h, http.MethodGet, "/admin/access", "").Body.String()
	if n := strings.Count(body, "data-principal-picker"); n < 2 {
		t.Errorf("access page has %d pickers, want at least 2 (webhooks, messages)", n)
	}
}

func TestLevelOf(t *testing.T) {
	t.Parallel()

	row := func(id string, actions ...string) models.Permission {
		return models.Permission{ResourceType: "Webhook", ResourceID: id, Actions: actions}
	}
	tests := []struct {
		rows []models.Permission
		want string
	}{
		{nil, "none"},
		{[]models.Permission{row("alertmanager", "use")}, "alertmanager"},
		{[]models.Permission{row("universal", "use")}, "universal"},
		{[]models.Permission{row("alertmanager", "use"), row("universal", "use")}, "all"},
		{[]models.Permission{row("*", "use")}, "all"},
		{[]models.Permission{row("*", "use", "administer")}, "admin"},
	}
	for _, tt := range tests {
		if got := levelOf(tt.rows); got != tt.want {
			t.Errorf("levelOf(%+v) = %s, want %s", tt.rows, got, tt.want)
		}
	}
}

func TestWebhookAdminsSetLevelsButSeeNoOverview(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	grantTo(st, models.PrincipalUser, "tester", "Webhook", "*", "use", "administer")
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	if loc := postFormAs(t, h, "/admin/access/webhooks", url.Values{"principal_type": {"user"}, "principal_id": {"s-bob"}, "level": {"alertmanager"}}).Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Errorf("a webhook admin could not set a level: %s", loc)
	}
	// The page opens to anyone holding a grant, but a webhook admin has no records to share
	// and none of the admins' sections.
	rec := call(t, h, http.MethodGet, "/admin/access", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/access as a webhook admin = %d, want 200", rec.Code)
	}
	for _, unwanted := range []string{"Snapshot generation", `action="/admin/access/webhooks"`} {
		if strings.Contains(rec.Body.String(), unwanted) {
			t.Errorf("a webhook admin sees %q", unwanted)
		}
	}

	editor := newTestServer(t, sessionAs(newFakeStore(), authz.RoleEditor), &fakeMessenger{}).Handler
	if rec := postFormAs(t, editor, "/admin/access/webhooks", url.Values{"level": {"all"}}); rec.Code != http.StatusForbidden {
		t.Errorf("an editor setting webhook levels = %d, want 403", rec.Code)
	}
}

func TestAccessPage(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.users["s-bob"] = models.User{Subject: "s-bob", Name: "Bob", Roles: []string{"viewer"}, IdPGroups: []string{"platform"}}
	st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
	st.members = []models.GroupMember{{GroupID: "g-ops", Type: models.MemberIdPGroup, ID: "platform"}}
	grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "update")
	grantTo(st, models.PrincipalUser, "s-bob", "Webhook", "*", "use")
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	tests := []struct {
		name     string
		query    string
		want     []string
		unwanted []string
	}{
		{
			name:     "the overview",
			want:     []string{"Bob (s-bob)", "both webhooks", "Ops (g-ops)", "Template t1", `Role::&#34;admin&#34;`, `principal in Group::&#34;g-ops&#34;`, "Snapshot generation 2", "Every grant"},
			unwanted: []string{"Grants you may change"},
		},
		{name: "filtered", query: "?filter=webhook", want: []string{"Webhook *"}, unwanted: []string{"Template t1"}},
		{
			name: "who can, through a provider group", query: "?subject=s-bob&action=update&resource=Template:t1",
			want: []string{"Allowed (1)", "Bob", "group Ops"},
		},
		{name: "who cannot", query: "?subject=s-bob&action=delete&resource=Template:t1", want: []string{"Nobody signed in may."}},
		{name: "an unknown subject", query: "?subject=ghost&action=read&resource=Template:t1", want: []string{"Nobody signed in may."}},
	}
	for _, tt := range tests {
		body := call(t, h, http.MethodGet, "/admin/access"+tt.query, "").Body.String()
		for _, want := range tt.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s: page lacks %q", tt.name, want)
			}
		}
		for _, unwanted := range tt.unwanted {
			if strings.Contains(body, unwanted) {
				t.Errorf("%s: page shows %q", tt.name, unwanted)
			}
		}
	}
	if rec := call(t, h, http.MethodPost, "/admin/access", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/access = %d", rec.Code)
	}
	failing := newTestServer(t, sessionAs(newFakeStore(), authz.RoleAdmin).fail("ListPermissions"), &fakeMessenger{}).Handler
	if rec := call(t, failing, http.MethodGet, "/admin/access", ""); rec.Code != http.StatusServiceUnavailable && !strings.Contains(rec.Body.String(), "Something went wrong") {
		t.Errorf("a failing store = %d", rec.Code)
	}
	if body := call(t, h, http.MethodGet, "/admin/templates", "").Body.String(); !strings.Contains(body, `href="/admin/access"`) || !strings.Contains(body, `href="/admin/me"`) {
		t.Error("the nav does not offer the access pages")
	}
}

func TestMyAccess(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	st.sessions[testSessionID] = func() models.Session {
		s := st.sessions[testSessionID]
		s.Identity.Groups = []string{"platform"}
		return s
	}()
	st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
	st.members = []models.GroupMember{{GroupID: "g-ops", Type: models.MemberIdPGroup, ID: "platform"}}
	grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "read")
	grantTo(st, models.PrincipalUser, "tester", "Webhook", "alertmanager", "use")
	grantTo(st, models.PrincipalUser, "someone-else", "Template", "t2", "own")
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := call(t, h, http.MethodGet, "/admin/me", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/me = %d", rec.Code)
	}
	for _, want := range []string{"tester", "platform", "Ops (g-ops)", "alertmanager", `Template::&#34;t1&#34;`, "People you may message", "yourself"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, `Template::&#34;t2&#34;`) || strings.Contains(body, "universal") {
		t.Error("the page shows what is not the viewer's")
	}

	nothing := newTestServer(t, sessionAs(newFakeStore()), &fakeMessenger{}).Handler
	if body := call(t, nothing, http.MethodGet, "/admin/me", "").Body.String(); !strings.Contains(body, "Nothing has been shared with you.") {
		t.Error("an empty page does not say so")
	}
	failing := newTestServer(t, sessionAs(newFakeStore()).fail("AuthzGeneration"), &fakeMessenger{}).Handler
	if rec := call(t, failing, http.MethodGet, "/admin/me", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("an unreadable generation = %d, want 503", rec.Code)
	}
}

// TestMessageLevels: admins grant naming anyone; self needs no grant (ADR 0082).
func TestMessageLevels(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	peopleRows := func() []string {
		var got []string
		for _, p := range st.permissions {
			if p.ResourceType == authz.PeopleResource.Type {
				got = append(got, p.PrincipalID+" "+strings.Join(p.Actions, " "))
			}
		}
		return got
	}

	form := url.Values{"principal_type": {"idp_group"}, "principal_id": {"office"}, "level": {"anyone"}}
	if loc := postFormAs(t, h, "/admin/access/messages", form).Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Fatalf("granting anyone redirected to %s", loc)
	}
	if got := peopleRows(); strings.Join(got, ",") != "office message" {
		t.Errorf("rows = %v, want office message", got)
	}
	if body := call(t, h, http.MethodGet, "/admin/access", "").Body.String(); !strings.Contains(body, "Who may message people") || !strings.Contains(body, "office") {
		t.Error("the overview does not list the message grant")
	}
	if rec := call(t, h, http.MethodPut, "/api/access/messages", `{"principal_type":"idp_group","principal_id":"office","level":"everyone"}`); rec.Code != http.StatusOK {
		t.Errorf("PUT everyone = %d %s", rec.Code, rec.Body.String())
	}
	if got := peopleRows(); strings.Join(got, ",") != "office broadcast" {
		t.Errorf("rows = %v, want office broadcast", got)
	}
	if body := call(t, h, http.MethodGet, "/admin/access", "").Body.String(); !strings.Contains(body, "broadcast to everyone") {
		t.Error("the overview does not name the everyone level")
	}
	if rec := call(t, h, http.MethodPut, "/api/access/messages", `{"principal_type":"idp_group","principal_id":"office","level":"none"}`); rec.Code != http.StatusOK {
		t.Errorf("PUT none = %d %s", rec.Code, rec.Body.String())
	}
	if got := peopleRows(); len(got) != 0 {
		t.Errorf("none left %v", got)
	}

	for _, body := range []string{
		`{"principal_type":"role","principal_id":"viewer","level":"self"}`,
		`{"principal_type":"robot","principal_id":"r","level":"anyone"}`,
		`{`,
	} {
		if rec := call(t, h, http.MethodPut, "/api/access/messages", body); rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want 400", body, rec.Code)
		}
	}
	if rec := call(t, h, http.MethodGet, "/api/access/messages", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", rec.Code)
	}
	editor := newTestServer(t, sessionAs(newFakeStore(), authz.RoleEditor), &fakeMessenger{}).Handler
	if rec := postFormAs(t, editor, "/admin/access/messages", form); rec.Code != http.StatusForbidden {
		t.Errorf("an editor granting message levels = %d, want 403", rec.Code)
	}
}

// TestLevelsRefuseUnknownGroups: a group named by its name rather than its id
// was saved, matched nobody and still looked granted.
func TestLevelsRefuseUnknownGroups(t *testing.T) {
	t.Parallel()

	tests := []struct{ path, level, resource string }{
		{"/admin/access/webhooks", "all", "Webhook"},
		{"/admin/access/messages", "everyone", "People"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(newFakeStore(), authz.RoleAdmin)
			st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "it-admins"}
			h := newTestServer(t, st, &fakeMessenger{}).Handler

			form := url.Values{"principal_type": {"group"}, "principal_id": {"it-admins"}, "level": {tt.level}}
			if loc := postFormAs(t, h, tt.path, form).Header().Get("Location"); !strings.Contains(loc, "error=") {
				t.Errorf("a group by name was accepted, redirected to %s", loc)
			}
			body := `{"principal_type":"group","principal_id":"it-admins","level":"` + tt.level + `"}`
			if rec := call(t, h, http.MethodPut, "/api"+strings.TrimPrefix(tt.path, "/admin"), body); rec.Code != http.StatusBadRequest {
				t.Errorf("PUT a group by name = %d, want 400", rec.Code)
			}
			if len(st.permissions) != 0 {
				t.Errorf("rows were saved: %v", st.permissions)
			}
			form.Set("principal_id", "g-ops")
			if loc := postFormAs(t, h, tt.path, form).Header().Get("Location"); strings.Contains(loc, "error=") {
				t.Errorf("the group by id was refused: %s", loc)
			}
			// Each row carries its own revoke, so removing it needs no retyped id.
			if body := call(t, h, http.MethodGet, "/admin/access", "").Body.String(); !strings.Contains(body, `<input type="hidden" name="principal_id" value="g-ops">`) {
				t.Error("the granted row has no revoke form")
			}

			// A row saved before the check must still be clearable.
			grantTo(st, models.PrincipalGroup, "it-admins", "People", "*", "broadcast")
			grantTo(st, models.PrincipalGroup, "it-admins", "Webhook", "*", "use")
			form = url.Values{"principal_type": {"group"}, "principal_id": {"it-admins"}, "level": {"none"}}
			if loc := postFormAs(t, h, tt.path, form).Header().Get("Location"); strings.Contains(loc, "error=") {
				t.Errorf("clearing a group by name was refused: %s", loc)
			}
			for _, p := range st.permissions {
				if p.PrincipalID == "it-admins" && p.ResourceType == tt.resource {
					t.Errorf("none left %v", p)
				}
			}
		})
	}
}

// TestAccessMenuFollowsShare: share comes only from admin or a grant row, so the nav and the gate agree (#233).
func TestAccessMenuFollowsShare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, st *fakeStore, h http.Handler)
		roles []authz.Role
		want  bool
	}{
		{name: "admin", roles: []authz.Role{authz.RoleAdmin}, want: true},
		{name: "user a grant names", want: true, setup: func(_ *testing.T, st *fakeStore, _ http.Handler) {
			grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "share")
		}},
		{name: "editor owning what they created", roles: []authz.Role{authz.RoleEditor}, want: true, setup: func(t *testing.T, _ *fakeStore, h http.Handler) {
			if rec := call(t, h, http.MethodPost, "/api/templates", `{"name":"Mine","body":"{}"}`); rec.Code != http.StatusCreated {
				t.Fatalf("create = %d", rec.Code)
			}
		}},
		{name: "role a share grant names", roles: []authz.Role{"oncall"}, want: true, setup: func(_ *testing.T, st *fakeStore, _ http.Handler) {
			grantTo(st, models.PrincipalRole, "oncall", "Template", "t1", "share")
		}},
		{name: "editor without grants", roles: []authz.Role{authz.RoleEditor}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(newFakeStore(), tt.roles...)
			st.templates["t1"] = models.Template{ID: "t1", Name: "Card", Body: `{"type":"AdaptiveCard"}`}
			h := newTestServer(t, st, &fakeMessenger{}).Handler
			if tt.setup != nil {
				tt.setup(t, st, h)
			}
			if got := strings.Contains(call(t, h, http.MethodGet, "/admin/templates", "").Body.String(), `href="/admin/access"`); got != tt.want {
				t.Errorf("menu offers the access page = %v, want %v", got, tt.want)
			}
			if got := call(t, h, http.MethodGet, "/admin/access", "").Code == http.StatusOK; got != tt.want {
				t.Errorf("access page opens = %v, want %v", got, tt.want)
			}
		})
	}
}

// whatPanel is the answer of the "what" tab and nothing else on the page.
func whatPanel(body string) string {
	_, after, _ := strings.Cut(body, `id="panel-what"`)
	panel, _, _ := strings.Cut(after, `id="panel-who"`)
	return panel
}

func TestWhatMayCheck(t *testing.T) {
	t.Parallel()

	newStore := func(roles ...authz.Role) *fakeStore {
		st := sessionAs(newFakeStore(), roles...)
		st.users["s-bob"] = models.User{Subject: "s-bob", Name: "Bob", Roles: []string{"viewer"}, IdPGroups: []string{"platform"}}
		st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
		st.groups["g-dev"] = models.Group{ID: "g-dev", Name: "Dev"}
		st.templates["t1"] = models.Template{ID: "t1", Name: "Card one", Body: `{"type":"AdaptiveCard"}`}
		st.templates["t2"] = models.Template{ID: "t2", Name: "Card two", Body: `{"type":"AdaptiveCard"}`}
		grantTo(st, models.PrincipalGroup, "g-ops", authz.PeopleResource.Type, authz.PeopleResource.ID, authz.ActionBroadcast)
		grantTo(st, models.PrincipalIdPGroup, "platform", "Template", "t1", "update")
		return st
	}

	tests := []struct {
		name     string
		store    *fakeStore
		query    string
		want     []string
		unwanted []string
		whole    bool // the notice sits outside the what panel
	}{
		{name: "group with a broadcast grant", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=group:g-ops", want: []string{"broadcast to everyone"}},
		{name: "group without one", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=group:g-dev", unwanted: []string{"broadcast to everyone"}},
		{name: "role", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=role:viewer", want: []string{"viewer</span>:"}},
		{name: "user takes stored roles", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=user:s-bob", want: []string{"viewer</span>:"}},
		{name: "user through a provider group", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=user:s-bob", want: []string{"Card one"}, unwanted: []string{"Card two"}},
		{name: "provider group", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=idp_group:platform", want: []string{"Card one"}, unwanted: []string{"Card two", "viewer</span>:"}},
		{name: "unknown user", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=user:ghost", want: []string{"Nobody has signed in with that subject"}},
		{name: "unknown group", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=group:nope", want: []string{"Nothing is known by that name."}},
		{name: "unknown role", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=role:nope", want: []string{"Nothing is known by that name."}},
		{name: "unknown kind", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=robot:r2", want: []string{"Nothing is known by that name."}},
		{name: "not the what tab", store: newStore(authz.RoleAdmin), query: "?principal=group:g-ops", unwanted: []string{"broadcast to everyone"}},
		{name: "not an admin", store: newStore(authz.RoleEditor), query: "?tab=what&principal=group:g-ops", unwanted: []string{"broadcast to everyone"}},
		{name: "a failing store", store: newStore(authz.RoleAdmin).fail("GetGroup"), query: "?tab=what&principal=group:g-ops", want: []string{"Something went wrong"}, whole: true},
		{name: "someone not listed", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=&other_type=user&other_id=%20s-bob%20", want: []string{"Card one", "viewer</span>:"}},
		{name: "the picker wins over not listed", store: newStore(authz.RoleAdmin), query: "?tab=what&principal=role:viewer&other_type=user&other_id=s-bob", want: []string{"viewer</span>:"}, unwanted: []string{"Card one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t, tt.store, &fakeMessenger{}).Handler
			panel := call(t, h, http.MethodGet, "/admin/access"+tt.query, "").Body.String()
			if !tt.whole {
				panel = whatPanel(panel)
			}
			for _, want := range tt.want {
				if !strings.Contains(panel, want) {
					t.Errorf("answer lacks %q", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(panel, unwanted) {
					t.Errorf("answer shows %q", unwanted)
				}
			}
		})
	}

	h := newTestServer(t, newStore(authz.RoleAdmin), &fakeMessenger{}).Handler
	if body := call(t, h, http.MethodGet, "/admin/access", "").Body.String(); !strings.Contains(body, `href="/admin/access?tab=what"`) || !strings.Contains(body, `id="panel-who"`) {
		t.Error("the default page lacks the tab bar")
	}
	// tabs.js switches to the who tab in place, so its form must be ready.
	_, who, _ := strings.Cut(call(t, h, http.MethodGet, "/admin/access?tab=what&principal=group:g-ops", "").Body.String(), `id="panel-who"`)
	if !strings.Contains(who, `<option value="read"`) {
		t.Error("the who form has no actions after a what question")
	}
	if strings.Count(who, ">Narrow to<") != 1 || strings.Contains(who, ">Give to<") {
		t.Error("the narrowing picker is not labelled once as Narrow to")
	}
	if !strings.Contains(whatPanel(call(t, h, http.MethodGet, "/admin/access?tab=what", "").Body.String()), `<span class="text-sm font-medium">Who</span>`) {
		t.Error("the what picker is not labelled Who")
	}
}

func TestLevelRowsNameWithoutID(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
	grantTo(st, models.PrincipalGroup, "g-ops", authz.PeopleResource.Type, authz.PeopleResource.ID, authz.ActionBroadcast)
	grantTo(st, models.PrincipalGroup, "g-ops", "Webhook", authz.WebhookUniversal, authz.ActionUse)
	body := call(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/access", "").Body.String()

	rows := strings.Split(body, `<span title="g-ops">`)[1:]
	if len(rows) != 2 {
		t.Fatalf("level rows with the id on hover = %d, want 2", len(rows))
	}
	for _, row := range rows {
		label, _, _ := strings.Cut(row, "</li>")
		if !strings.Contains(label, "Ops") || strings.Contains(label, "(g-ops)") {
			t.Errorf("level row = %q, want the name without the id", label)
		}
	}
}
