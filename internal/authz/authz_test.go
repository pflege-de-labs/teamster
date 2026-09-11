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

	admin := []string{"teamster-admins"}
	editor := []string{"teamster-editors", "oncall"}
	viewer := []string{"everyone"}

	tests := []struct {
		name   string
		values []string
		want   Role
	}{
		{name: "an admin value wins", values: []string{"everyone", "teamster-admins"}, want: RoleAdmin},
		{name: "an editor value beats a viewer one", values: []string{"everyone", "oncall"}, want: RoleEditor},
		{name: "a viewer value", values: []string{"everyone"}, want: RoleViewer},
		{
			// Signed in but named by no list: least privilege, not most.
			name: "no value at all", values: []string{"unrelated"}, want: RoleViewer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RoleFor(tt.values, admin, editor, viewer); got != tt.want {
				t.Errorf("RoleFor(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
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
