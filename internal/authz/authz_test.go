package authz

import (
	"slices"
	"testing"
)

func TestAllow(t *testing.T) {
	t.Parallel()

	authorizer, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name     string
		roles    []Role
		action   string
		resource Resource
		want     bool
	}{
		{name: "an admin may edit", roles: []Role{RoleAdmin}, action: ActionEdit, resource: Resource{Type: "Template", ID: "t1"}, want: true},
		{name: "an admin may administer", roles: []Role{RoleAdmin}, action: ActionAdminister, resource: Resource{Type: "Grant"}, want: true},
		{name: "an editor may edit", roles: []Role{RoleEditor}, action: ActionEdit, resource: Resource{Type: "Route", ID: "r1"}, want: true},
		{name: "an editor inherits viewing", roles: []Role{RoleEditor}, action: ActionView, resource: Resource{Type: "Route"}, want: true},
		{
			// Managing who may do what is the admin's, not the editor's.
			name: "an editor may not administer", roles: []Role{RoleEditor}, action: ActionAdminister,
			resource: Resource{Type: "Grant"}, want: false,
		},
		{name: "a viewer may view", roles: []Role{RoleViewer}, action: ActionView, resource: Resource{Type: "Destination"}, want: true},
		{name: "a viewer may not edit", roles: []Role{RoleViewer}, action: ActionEdit, resource: Resource{Type: "Destination", ID: "d1"}, want: false},
		{
			// A role this build does not know must not fall through to allowed.
			name: "an unknown role decides nothing", roles: []Role{Role("superuser")}, action: ActionView,
			resource: Resource{Type: "Template"}, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := authorizer.Allow("subject", tt.roles, tt.action, tt.resource); got != tt.want {
				t.Errorf("Allow(%v, %q, %+v) = %v, want %v", tt.roles, tt.action, tt.resource, got, tt.want)
			}
		})
	}
}

func TestRolesFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		values      []string
		defaultRole Role
		want        []Role
	}{
		// The provider's role names are the roles here, one to one.
		{name: "admin by name", values: []string{"admin"}, want: []Role{RoleAdmin}},
		{name: "editor by name", values: []string{"editor"}, want: []Role{RoleEditor}},
		{
			// Both are held; the policies decide, and admin permits more.
			name: "several at once", values: []string{"viewer", "admin"},
			want: []Role{RoleViewer, RoleAdmin},
		},
		{
			// A role this build never heard of reaches the policies unchanged,
			// so a deployment can write its own and have it mean something.
			name: "a role of the deployment's own", values: []string{"auditor", "editor"},
			want: []Role{"auditor", RoleEditor},
		},
		{
			// Keycloak gives everyone offline_access, so a claim is almost never
			// empty: the default fills in for a missing Teamster role, not for a
			// missing claim, and the realm's own roles are kept beside it.
			name: "no Teamster role falls back to the default", values: []string{"offline_access"},
			defaultRole: RoleViewer, want: []Role{"offline_access", RoleViewer},
		},
		{name: "no Teamster role and no default", values: []string{"offline_access"}, want: []Role{"offline_access"}},
		{name: "no claim values at all", want: nil},
		{name: "no values but a default", defaultRole: RoleEditor, want: []Role{RoleEditor}},
		{
			name: "a nonsense default is not honoured", values: []string{"offline_access"},
			defaultRole: Role("superuser"), want: []Role{"offline_access"},
		},
		{name: "duplicates collapse", values: []string{"editor", "editor"}, want: []Role{RoleEditor}},
		{name: "blank values are dropped", values: []string{"", "  ", "viewer"}, want: []Role{RoleViewer}},
		{
			// "none" is the marker for an empty set in storage, not a role a
			// provider can hand out.
			name: "the none marker is not a claimable role", values: []string{"none"}, want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RolesFor(tt.values, tt.defaultRole); !slices.Equal(got, tt.want) {
				t.Errorf("RolesFor(%v, %q) = %v, want %v", tt.values, tt.defaultRole, got, tt.want)
			}
		})
	}
}

func TestHasBuiltin(t *testing.T) {
	t.Parallel()

	if !HasBuiltin([]Role{"auditor", RoleViewer}) {
		t.Error("HasBuiltin() = false with a viewer among the roles")
	}
	if HasBuiltin([]Role{"auditor", "oncall"}) {
		t.Error("HasBuiltin() = true with only roles of the deployment's own")
	}
	if HasBuiltin(nil) {
		t.Error("HasBuiltin(nil) = true")
	}
}

// Storage round trip: a session row says what its holder may do.
func TestEncodeDecode(t *testing.T) {
	t.Parallel()

	if got := Encode(nil); got != string(RoleNone) {
		t.Errorf("Encode(nil) = %q, want %q", got, RoleNone)
	}
	if got := Decode(string(RoleNone)); got != nil {
		t.Errorf("Decode(%q) = %v, want nil", RoleNone, got)
	}

	roles := []Role{RoleAdmin, "auditor"}
	if got := Decode(Encode(roles)); !slices.Equal(got, roles) {
		t.Errorf("Decode(Encode(%v)) = %v", roles, got)
	}
}

// A role no policy mentions grants nothing, which is what makes handing every
// claim value to Cedar safe.
func TestARoleNoPolicyMentionsGrantsNothing(t *testing.T) {
	t.Parallel()

	authorizer, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if authorizer.Allow("subject", []Role{"auditor"}, ActionView, Resource{Type: "Template"}) {
		t.Error("a role with no policy may view; want nothing permitted")
	}
}

// RoleNone is a role, but it is not a valid one to hold: nothing permits it.
func TestNoRoleIsPermittedNothing(t *testing.T) {
	t.Parallel()

	authorizer, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if authorizer.Allow("subject", nil, ActionView, Resource{Type: "Template"}) {
		t.Error("a user with no role may view; want nothing permitted")
	}
}

func TestValid(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RoleAdmin, RoleEditor, RoleViewer} {
		if !Valid(role) {
			t.Errorf("Valid(%q) = false, want true", role)
		}
	}
	for _, role := range []Role{"", "superuser", "Admin"} {
		if Valid(role) {
			t.Errorf("Valid(%q) = true, want false", role)
		}
	}
}
