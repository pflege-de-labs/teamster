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
	st.users["s-seen"] = models.User{Subject: "s-seen", Name: "Seen", IdPGroups: []string{"oncall"}}
	grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "own")
	grantTo(st, models.PrincipalUser, "tester", "Template", "t3", "own")
	st.templates["t4"] = models.Template{ID: "t4", Name: "Fourth card", Body: "{}"}
	grantTo(st, models.PrincipalUser, "tester", "Template", "t4", "read", "share")
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
		{name: "classes", want: []string{"Templates", `href="/admin/access"`, "Grants you may change"}, unwanted: []string{"Snapshot generation", "Webhook endpoints", "Every grant"}},
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
			wantError: "t2: you may not share this template", wantRows: 0,
		},
		{
			name:      "a refused record the viewer may share is named as listed",
			form:      url.Values{"resource_type": {"Template"}, "ids": {"t1", "t4"}, "principal": {"group:g-sre"}, "actions": {"update"}},
			wantError: "Fourth card: you may not update this template", wantRows: 0,
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

// TestShareWarnsAboutPrincipalsNotSeen checks the batch form and a record's own
// sharing panel warn alike.
func TestShareWarnsAboutPrincipalsNotSeen(t *testing.T) {
	t.Parallel()

	const notSeen, groupNotSeen = "has not signed in yet", "Nobody from this provider group"
	principals := []struct {
		name      string
		principal url.Values
		wantWarn  string
		wantError string
	}{
		{name: "known user", principal: url.Values{"principal": {"user:s-seen"}}},
		{name: "unknown user", principal: url.Values{"other_type": {"user"}, "other_id": {"s-new"}}, wantWarn: notSeen},
		{name: "known provider group", principal: url.Values{"principal": {"idp_group:oncall"}}},
		{name: "unknown provider group", principal: url.Values{"other_type": {"idp_group"}, "other_id": {" nightshift "}}, wantWarn: groupNotSeen},
		{name: "known local group", principal: url.Values{"principal": {"group:g-sre"}}},
		{name: "unknown local group", principal: url.Values{"principal": {"group:g-nobody"}}, wantError: "No such group"},
	}
	forms := []struct {
		name, path string
		record     url.Values
	}{
		{name: "batch", path: "/admin/sharing/batch", record: url.Values{"ids": {"t1"}}},
		{name: "record", path: "/admin/sharing/grant", record: url.Values{"resource_id": {"t1"}}},
	}
	for _, form := range forms {
		for _, tt := range principals {
			t.Run(form.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				values := url.Values{"resource_type": {"Template"}, "actions": {"read"}, "return": {"/admin/access?class=Template"}}
				for k, v := range form.record {
					values[k] = v
				}
				for k, v := range tt.principal {
					values[k] = v
				}
				h := newTestServer(t, hubStore(), &fakeMessenger{}).Handler
				loc, err := url.Parse(postFormAs(t, h, form.path, values).Header().Get("Location"))
				if err != nil {
					t.Fatal(err)
				}
				notice, gotError := loc.Query().Get("notice"), loc.Query().Get("error")
				if tt.wantError != "" {
					if !strings.Contains(gotError, tt.wantError) {
						t.Errorf("error = %q, want %q", gotError, tt.wantError)
					}
					return
				}
				if gotError != "" || !strings.HasPrefix(notice, "Permissions saved.") {
					t.Fatalf("notice = %q, error = %q", notice, gotError)
				}
				for _, warn := range []string{notSeen, groupNotSeen} {
					if strings.Contains(notice, warn) != (warn == tt.wantWarn) {
						t.Errorf("notice = %q, want warning %q", notice, tt.wantWarn)
					}
				}
			})
		}
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
