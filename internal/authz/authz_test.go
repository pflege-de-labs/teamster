package authz

import "testing"

func TestAllow(t *testing.T) {
	t.Parallel()

	authorizer, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name     string
		role     Role
		action   string
		resource Resource
		want     bool
	}{
		{name: "an admin may edit", role: RoleAdmin, action: ActionEdit, resource: Resource{Type: "Template", ID: "t1"}, want: true},
		{name: "an admin may administer", role: RoleAdmin, action: ActionAdminister, resource: Resource{Type: "Grant"}, want: true},
		{name: "an editor may edit", role: RoleEditor, action: ActionEdit, resource: Resource{Type: "Route", ID: "r1"}, want: true},
		{name: "an editor inherits viewing", role: RoleEditor, action: ActionView, resource: Resource{Type: "Route"}, want: true},
		{
			// Managing who may do what is the admin's, not the editor's.
			name: "an editor may not administer", role: RoleEditor, action: ActionAdminister,
			resource: Resource{Type: "Grant"}, want: false,
		},
		{name: "a viewer may view", role: RoleViewer, action: ActionView, resource: Resource{Type: "Destination"}, want: true},
		{name: "a viewer may not edit", role: RoleViewer, action: ActionEdit, resource: Resource{Type: "Destination", ID: "d1"}, want: false},
		{
			// A role this build does not know must not fall through to allowed.
			name: "an unknown role decides nothing", role: Role("superuser"), action: ActionView,
			resource: Resource{Type: "Template"}, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := authorizer.Allow("subject", tt.role, tt.action, tt.resource); got != tt.want {
				t.Errorf("Allow(%q, %q, %+v) = %v, want %v", tt.role, tt.action, tt.resource, got, tt.want)
			}
		})
	}
}

func TestRoleFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		values      []string
		defaultRole Role
		want        Role
	}{
		// The provider's role names are the roles here, with nothing to map.
		{name: "admin by name", values: []string{"admin"}, want: RoleAdmin},
		{name: "editor by name", values: []string{"editor"}, want: RoleEditor},
		{name: "viewer by name", values: []string{"viewer"}, want: RoleViewer},
		{name: "the most privileged name wins", values: []string{"viewer", "admin"}, want: RoleAdmin},
		{name: "unrelated roles are ignored", values: []string{"offline_access", "editor"}, want: RoleEditor},
		{
			name: "no role named falls back to the default", values: []string{"offline_access"},
			defaultRole: RoleViewer, want: RoleViewer,
		},
		{
			// Without a default, a user the claim says nothing about gets
			// nothing — and is told so rather than silently admitted.
			name: "no role and no default", values: []string{"offline_access"}, want: RoleNone,
		},
		{name: "no claim values at all", want: RoleNone},
		{
			name: "a nonsense default is not honoured", values: []string{"offline_access"},
			defaultRole: Role("superuser"), want: RoleNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RoleFor(tt.values, tt.defaultRole); got != tt.want {
				t.Errorf("RoleFor(%v, %q) = %q, want %q", tt.values, tt.defaultRole, got, tt.want)
			}
		})
	}
}

// RoleNone is a role, but it is not a valid one to hold: nothing permits it.
func TestNoRoleIsPermittedNothing(t *testing.T) {
	t.Parallel()

	authorizer, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if authorizer.Allow("subject", RoleNone, ActionView, Resource{Type: "Template"}) {
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
