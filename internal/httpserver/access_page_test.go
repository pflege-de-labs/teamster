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
			// Whatever was there before is replaced.
			grantTo(st, models.PrincipalGroup, "g-ops", "Webhook", "universal", "use")
			h := newTestServer(t, st, &fakeMessenger{}).Handler

			loc := postFormAs(t, h, "/admin/access/webhooks", url.Values{
				"principal_type": {"group"}, "principal_id": {"g-ops"}, "level": {tt.level},
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
			name: "the overview",
			want: []string{"Bob (s-bob)", "both webhooks", "Ops (g-ops)", "Template t1", `Role::&#34;admin&#34;`, `principal in Group::&#34;g-ops&#34;`, "Snapshot generation 2"},
		},
		{name: "filtered", query: "?filter=webhook", want: []string{"Webhook *"}, unwanted: []string{"Template t1"}},
		{
			name: "who can, through a provider group", query: "?subject=s-bob&action=update&resource=Template:t1",
			want: []string{"Allowed, by these policies:", "perm:seed-group-g-ops-Template-t1"},
		},
		{name: "who cannot", query: "?subject=s-bob&action=delete&resource=Template:t1", want: []string{"Refused"}},
		{name: "an unknown subject", query: "?subject=ghost&action=read&resource=Template:t1", want: []string{"Nobody has signed in with that subject"}},
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
	if body := call(t, h, http.MethodGet, "/admin", "").Body.String(); !strings.Contains(body, `href="/admin/access"`) || !strings.Contains(body, `href="/admin/me"`) {
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
