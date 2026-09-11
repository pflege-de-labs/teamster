package httpserver

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func scopesOf(st *fakeStore, role string) []string {
	var out []string
	for _, grant := range st.grants {
		if grant.Role != role {
			continue
		}
		out = append(out, grant.TeamID+"/"+grant.ChannelID)
	}
	sort.Strings(out)
	return out
}

// The tree is edited whole, so saving it replaces what the role had rather than
// adding to it — and a half-applied scope is not a state anyone should see.
func TestReplacingWhatARoleMayReach(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		want  []string
		other []string
	}{
		{
			name: "a whole Team",
			body: `{"role":"editor","scopes":[{"team_id":"platform"}]}`,
			want: []string{"platform/"},
		},
		{
			name: "channels one by one",
			body: `{"role":"editor","scopes":[{"team_id":"platform","channel_id":"alerts"},{"team_id":"platform","channel_id":"incidents"}]}`,
			want: []string{"platform/alerts", "platform/incidents"},
		},
		{
			// Nothing ticked means the role is unrestricted again, which is what
			// no grants means everywhere else.
			name: "nothing at all",
			body: `{"role":"editor","scopes":[]}`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), authz.RoleAdmin)
			st.grants["old"] = models.Grant{ID: "old", Role: "editor", TeamID: "payments"}
			st.grants["other"] = models.Grant{ID: "other", Role: "viewer", TeamID: "payments"}

			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPut, "/api/grants/role", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("PUT = %d, want 200 (%s)", rec.Code, rec.Body.String())
			}

			if got := scopesOf(st, "editor"); !equalStrings(got, tt.want) {
				t.Errorf("editor reaches %v, want %v", got, tt.want)
			}
			// Another role's grants are not this role's to remove.
			if got := scopesOf(st, "viewer"); !equalStrings(got, []string{"payments/"}) {
				t.Errorf("viewer reaches %v, want its own grant untouched", got)
			}
		})
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestReplacingGrantsIsRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       authz.Role
		method     string
		body       string
		wantStatus int
	}{
		{
			name: "without a role", role: authz.RoleAdmin, method: http.MethodPut,
			body: `{"scopes":[{"team_id":"platform"}]}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "with a scope naming no Team", role: authz.RoleAdmin, method: http.MethodPut,
			body: `{"role":"editor","scopes":[{"channel_id":"alerts"}]}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "not JSON", role: authz.RoleAdmin, method: http.MethodPut,
			body: `{`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "by the wrong method", role: authz.RoleAdmin, method: http.MethodPost,
			body: `{"role":"editor"}`, wantStatus: http.StatusMethodNotAllowed,
		},
		{
			// Deciding who may deliver where is the admin's.
			name: "by an editor", role: authz.RoleEditor, method: http.MethodPut,
			body: `{"role":"editor","scopes":[]}`, wantStatus: http.StatusForbidden,
		},
		{
			name: "by a viewer", role: authz.RoleViewer, method: http.MethodPut,
			body: `{"role":"viewer","scopes":[]}`, wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), tt.role)
			st.grants["kept"] = models.Grant{ID: "kept", Role: "editor", TeamID: "payments"}

			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.method, "/api/grants/role", tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("%s = %d, want %d (%s)", tt.method, rec.Code, tt.wantStatus, rec.Body.String())
			}
			if len(st.grants) != 1 {
				t.Errorf("grants = %+v, want the refused request to have changed nothing", st.grants)
			}
		})
	}
}

// A store that fails half way through must leave the role's scope as it was,
// not partly replaced.
func TestAFailedReplacementKeepsTheOldScope(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin).fail("CreateGrant")
	st.grants["old"] = models.Grant{ID: "old", Role: "editor", TeamID: "payments"}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPut, "/api/grants/role",
		`{"role":"editor","scopes":[{"team_id":"platform"}]}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT = %d, want 500", rec.Code)
	}
	if got := scopesOf(st, "editor"); !equalStrings(got, []string{"payments/"}) {
		t.Errorf("editor reaches %v, want the scope it had before", got)
	}
}

func TestPermissionsPage(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	st.grants["g1"] = models.Grant{ID: "g1", Role: "payments-editors", TeamID: "platform"}

	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/permissions", "").Body.String()

	for _, want := range []string{
		`id="permissions-tree"`, `id="permissions-all"`, `id="permissions-role"`,
		`src="/permissions.js"`, "Which Teams and channels",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the permissions page is missing %q", want)
		}
	}

	// The scopeable built-in roles and any a grant already names, so a
	// deployment finds the role it invented rather than having to spell it again.
	for _, role := range []string{"editor", "viewer", "payments-editors"} {
		if !strings.Contains(body, `value="`+role+`"`) {
			t.Errorf("the role list does not offer %q", role)
		}
	}
	// Admin is not offered: the admin policy permits everything, so a scope set
	// for it would be stored and then ignored.
	if strings.Contains(body, `value="admin"`) {
		t.Error("the role list offers admin, which cannot be scoped")
	}
	if !strings.Contains(body, "Admins are not listed") {
		t.Error("the page does not say why admin is absent")
	}
}

func TestPermissionsPageIsAdminOnly(t *testing.T) {
	t.Parallel()

	for _, role := range []authz.Role{authz.RoleEditor, authz.RoleViewer} {
		st := sessionAs(seededUIStore(), role)
		handler := newTestServer(t, st, &fakeMessenger{}).Handler

		if rec := asRole(t, handler, http.MethodGet, "/admin/permissions", ""); rec.Code != http.StatusForbidden {
			t.Errorf("GET /admin/permissions as %s = %d, want 403", role, rec.Code)
		}
		// And it is not in the navigation either, because offering a link that
		// answers 403 is its own kind of broken.
		if page := asRole(t, handler, http.MethodGet, "/admin", "").Body.String(); strings.Contains(page, `href="/admin/permissions"`) {
			t.Errorf("a %s is offered the permissions tab", role)
		}
	}
}

// The tree is built in the browser from the pickers, which are already scoped;
// what the page itself must carry is the current grants.
func TestGrantsAreReadableForTheTree(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.grants["g1"] = models.Grant{ID: "g1", Role: "editor", TeamID: "platform", ChannelID: "alerts"}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/api/grants", "")
	var grants []models.Grant
	if err := json.Unmarshal(rec.Body.Bytes(), &grants); err != nil {
		t.Fatalf("decode grants: %v", err)
	}
	if len(grants) != 1 || grants[0].ChannelID != "alerts" {
		t.Errorf("grants = %+v, want the stored scope", grants)
	}
}

// A grant naming admin, however it got there, does not make admin scopeable.
func TestAdminIsNeverOfferedAsAScopeableRole(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	st.grants["legacy"] = models.Grant{ID: "legacy", Role: "admin", TeamID: "platform"}

	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/permissions", "").Body.String()
	if strings.Contains(body, `value="admin"`) {
		t.Error("a stored admin grant put admin back in the list")
	}
}
