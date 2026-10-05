package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// hubStore is a session without a role that owns t1 and t3 but not t2.
func hubStore() *fakeStore {
	st := sharedStore()
	st.templates["t3"] = models.Template{ID: "t3", Name: "Third card", Body: "{}"}
	st.groups["g-sre"] = models.Group{ID: "g-sre", Name: "SRE"}
	grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "own")
	grantTo(st, models.PrincipalUser, "tester", "Template", "t3", "own")
	return st
}

func TestAccessHubListsWhatMayBeShared(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, hubStore(), &fakeMessenger{}).Handler
	tests := []struct {
		name     string
		query    string
		want     []string
		unwanted []string
	}{
		{name: "classes", want: []string{"Templates", `href="/admin/access"`}, unwanted: []string{"Snapshot generation", "Webhook endpoints"}},
		{
			name: "records of a class", query: "?class=Template",
			want:     []string{"Shared card", "Third card", `name="ids" value="t1"`, `action="/admin/sharing/batch"`, `value="group:g-sre"`},
			unwanted: []string{"Private card"},
		},
		{name: "one record's grants", query: "?class=Template&id=t1", want: []string{`id="sharing"`, "tester"}},
		{name: "a record not shared by me", query: "?class=Template&id=t2", unwanted: []string{`id="sharing"`}},
	}
	for _, tt := range tests {
		rec := call(t, h, http.MethodGet, "/admin/access"+tt.query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET /admin/access%s = %d", tt.name, tt.query, rec.Code)
		}
		body := rec.Body.String()
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
}

func TestShareBatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		form       url.Values
		wantError  string
		wantNotice string
		wantRows   int
	}{
		{
			name:       "two records at once",
			form:       url.Values{"resource_type": {"Template"}, "ids": {"t1", "t3"}, "principal": {"group:g-sre"}, "actions": {"read", "update"}},
			wantNotice: "Permissions saved.", wantRows: 2,
		},
		{
			name:      "one refused record saves none",
			form:      url.Values{"resource_type": {"Template"}, "ids": {"t1", "t2"}, "principal": {"group:g-sre"}, "actions": {"read"}},
			wantError: "template t2", wantRows: 0,
		},
		{
			name:      "a group must exist",
			form:      url.Values{"resource_type": {"Template"}, "ids": {"t1"}, "principal": {"group:g-nobody"}, "actions": {"read"}},
			wantError: "No such group", wantRows: 0,
		},
		{
			name:       "a user not seen yet is accepted with a warning",
			form:       url.Values{"resource_type": {"Template"}, "ids": {"t1"}, "other_type": {"user"}, "other_id": {"s-new"}, "actions": {"read"}},
			wantNotice: "has not signed in yet", wantRows: 1,
		},
		{
			name:      "nothing picked",
			form:      url.Values{"resource_type": {"Template"}, "principal": {"group:g-sre"}, "actions": {"read"}},
			wantError: "at least one record", wantRows: 0,
		},
		{
			name:      "a collection action is not grantable on a record",
			form:      url.Values{"resource_type": {"Template"}, "ids": {"t1"}, "principal": {"group:g-sre"}, "actions": {"create"}},
			wantError: "a grant needs", wantRows: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := hubStore()
			h := newTestServer(t, st, &fakeMessenger{}).Handler
			tt.form.Set("return", "/admin/access?class=Template")
			loc, err := url.Parse(postFormAs(t, h, "/admin/sharing/batch", tt.form).Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if got := loc.Query().Get("error"); tt.wantError == "" && got != "" {
				t.Errorf("error = %q", got)
			} else if tt.wantError != "" && !strings.Contains(got, tt.wantError) {
				t.Errorf("error = %q, want %q", got, tt.wantError)
			}
			if got := loc.Query().Get("notice"); !strings.Contains(got, tt.wantNotice) {
				t.Errorf("notice = %q, want %q", got, tt.wantNotice)
			}
			rows := 0
			st.mu.Lock()
			for _, p := range st.permissions {
				if !strings.HasPrefix(p.ID, "seed-") {
					rows++
				}
			}
			st.mu.Unlock()
			if rows != tt.wantRows {
				t.Errorf("saved %d rows, want %d", rows, tt.wantRows)
			}
		})
	}
}

func TestMyAccessListsWhatWasShared(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, sharedStore(), &fakeMessenger{}).Handler
	body := call(t, h, http.MethodGet, "/admin/me", "").Body.String()
	for _, want := range []string{"Shared card", `href="/admin?edit=templates&amp;id=t1"`, "read, update"} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/me lacks %q", want)
		}
	}
	if strings.Contains(body, "Private card") {
		t.Error("/admin/me names a template nobody shared")
	}
}

func TestPickerOffersARoleADeploymentDefined(t *testing.T) {
	t.Parallel()

	st := hubStore()
	st.users["s-oncall"] = models.User{Subject: "s-oncall", Name: "Oli", Roles: []string{"oncall"}}
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	if body := call(t, h, http.MethodGet, "/admin/access?class=Template", "").Body.String(); !strings.Contains(body, `value="role:oncall"`) {
		t.Error("the picker does not offer the role a user was seen with")
	}
	loc := postFormAs(t, h, "/admin/sharing/batch", url.Values{
		"resource_type": {"Template"}, "ids": {"t1"}, "principal": {"role:oncall"}, "actions": {"read"}, "return": {"/admin/access"},
	}).Header().Get("Location")
	if strings.Contains(loc, "error=") {
		t.Errorf("granting to a custom role failed: %s", loc)
	}
}

func TestWhoCanAsksAboutWebhooksAndOldLinks(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.users["s-bob"] = models.User{Subject: "s-bob", Name: "Bob", Roles: []string{"viewer"}}
	grantTo(st, models.PrincipalUser, "s-bob", "Webhook", "alertmanager", "use")
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	tests := []struct {
		name, query, want string
	}{
		{"a webhook", "?subject=s-bob&action=use&resource=Webhook:alertmanager", "Allowed, by these policies:"},
		{"the other webhook", "?subject=s-bob&action=use&resource=Webhook:universal", "Refused"},
		{"an old link", "?subject=s-bob&action=use&type=Webhook&id=alertmanager", "Allowed, by these policies:"},
	}
	for _, tt := range tests {
		if body := call(t, h, http.MethodGet, "/admin/access"+tt.query, "").Body.String(); !strings.Contains(body, tt.want) {
			t.Errorf("%s: page lacks %q", tt.name, tt.want)
		}
	}
}

func TestMyAccessSummarisesRoles(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, sessionAs(newFakeStore(), authz.RoleEditor), &fakeMessenger{}).Handler
	body := call(t, h, http.MethodGet, "/admin/me", "").Body.String()
	for _, want := range []string{"editor", "read, change"} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/me lacks %q", want)
		}
	}
	if strings.Contains(body, "%!") || strings.Contains(body, "{0}") {
		t.Error("/admin/me has an unfilled placeholder")
	}
}
