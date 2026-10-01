package authz

import (
	"testing"

	cedar "github.com/cedar-policy/cedar-go"
)

// withPolicy adds a test policy to a snapshot built from the model.
func withPolicy(t *testing.T, model Model, text string) *Authorizer {
	t.Helper()
	a, err := build(1, model)
	if err != nil {
		t.Fatal(err)
	}
	var policy cedar.Policy
	if err := policy.UnmarshalCedar([]byte(text)); err != nil {
		t.Fatal(err)
	}
	a.policies.Add("test", &policy)
	return a
}

func TestGroupsReachThePrincipal(t *testing.T) {
	t.Parallel()

	model := Model{Members: []Membership{
		{Group: "oncall", Kind: "group", ID: "sre"},
		{Group: "sre", Kind: "user", ID: "bob"},
		{Group: "sre", Kind: "idp_group", ID: "platform"},
		{Group: "other", Kind: "user", ID: "carol"},
	}}
	a := withPolicy(t, model, `permit (principal in Group::"oncall", action == Action::"probe", resource);`)

	tests := []struct {
		name string
		p    Principal
		want bool
	}{
		{"a member of a member group", Principal{Subject: "bob"}, true},
		{"through the identity provider", Principal{Subject: "dave", IdPGroups: []string{"platform"}}, true},
		{"an unknown provider group", Principal{Subject: "dave", IdPGroups: []string{"marketing"}}, false},
		{"another group's member", Principal{Subject: "carol"}, false},
		{"nobody", Principal{}, false},
	}
	for _, tt := range tests {
		resource := Resource{Type: "Template", ID: "t"}
		if got := a.AllowFor(tt.p, "probe", resource); got != tt.want {
			t.Errorf("%s: AllowFor = %v, want %v", tt.name, got, tt.want)
		}
		if got := a.AllowScopedFor(tt.p, "probe", resource, Scope{Unrestricted: true}); got != tt.want {
			t.Errorf("%s: AllowScopedFor = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestGroupsDoNotChangeTheRoles(t *testing.T) {
	t.Parallel()

	a, err := build(1, Model{Members: []Membership{{Group: "g", Kind: "user", ID: "bob"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.AllowFor(Principal{Subject: "bob"}, ActionView, Resource{Type: "Template"}) {
		t.Error("a group membership granted a role's permission")
	}
	if !a.AllowFor(Principal{Subject: "bob", Roles: []Role{RoleViewer}}, ActionView, Resource{Type: "Template"}) {
		t.Error("a viewer in a group lost their role")
	}
}
