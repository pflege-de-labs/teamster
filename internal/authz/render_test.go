package authz

import (
	"strings"
	"testing"

	cedar "github.com/cedar-policy/cedar-go"
)

func TestGrantRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		grant Grant
		want  string
	}{
		{
			name:  "an owner",
			grant: Grant{ID: "p1", PrincipalType: "user", PrincipalID: "alice", ResourceType: "Template", ResourceID: "t1", Actions: []string{"own"}},
			want:  `@id("perm:p1")` + "\n" + `permit (` + "\n" + `    principal == User::"alice",` + "\n" + `    action in [Action::"own"],` + "\n" + `    resource == Template::"t1"` + "\n" + `);`,
		},
		{
			name:  "a group",
			grant: Grant{ID: "p2", PrincipalType: "group", PrincipalID: "g-sre", ResourceType: "Destination", ResourceID: "d7", Actions: []string{"read", "update"}},
			want:  `principal in Group::"g-sre"`,
		},
		{
			name:  "a provider group",
			grant: Grant{ID: "p3", PrincipalType: "idp_group", PrincipalID: "platform", ResourceType: "Route", ResourceID: "*", Actions: []string{"create"}},
			want:  `principal in IdpGroup::"platform"`,
		},
		{
			name:  "a role",
			grant: Grant{ID: "p4", PrincipalType: "role", PrincipalID: "auditor", ResourceType: "Group", ResourceID: "g1", Actions: []string{"read"}},
			want:  `principal in Role::"auditor"`,
		},
		{
			name:  "a hostile id stays a string",
			grant: Grant{ID: "p5", PrincipalType: "user", PrincipalID: `x", action, resource); permit (principal, action, resource`, ResourceType: "Template", ResourceID: "t\n1", Actions: []string{"read"}},
			want:  `User::"x\", action, resource); permit (principal, action, resource"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy, err := tt.grant.Render()
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			text := string(policy.MarshalCedar())
			if !strings.Contains(text, tt.want) {
				t.Errorf("rendered\n%s\nwant it to contain\n%s", text, tt.want)
			}
			// What is rendered parses back to one policy.
			set, err := cedar.NewPolicySetFromBytes("grant", []byte(text))
			if err != nil {
				t.Fatalf("the rendered text does not parse: %v", err)
			}
			n := 0
			for range set.All() {
				n++
			}
			if n != 1 {
				t.Errorf("the rendered text holds %d policies, want 1", n)
			}
		})
	}
}

func TestGrantValidate(t *testing.T) {
	t.Parallel()

	ok := Grant{ID: "p", PrincipalType: "user", PrincipalID: "a", ResourceType: "Template", ResourceID: "t", Actions: []string{"read"}}
	tests := []struct {
		name   string
		mutate func(*Grant)
		want   string
	}{
		{"no principal", func(g *Grant) { g.PrincipalID = "" }, "principal"},
		{"unknown principal type", func(g *Grant) { g.PrincipalType = "robot" }, "principal type"},
		{"a type with no permissions", func(g *Grant) { g.ResourceType = "Grant" }, "cannot be granted"},
		{"no actions", func(g *Grant) { g.Actions = nil }, "at least one"},
		{"an unknown action", func(g *Grant) { g.Actions = []string{"administer"} }, "unknown action"},
		{"create on a record", func(g *Grant) { g.Actions = []string{"create"} }, "collection"},
		{"read on the collection", func(g *Grant) { g.ResourceID = "*" }, "collection"},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a valid grant: %v", err)
	}
	for _, tt := range tests {
		g := ok
		g.Actions = append([]string(nil), ok.Actions...)
		tt.mutate(&g)
		if err := g.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate() = %v, want an error naming %q", tt.name, err, tt.want)
		}
		if _, err := g.Render(); err == nil {
			t.Errorf("%s: rendered anyway", tt.name)
		}
	}
}

func TestGrantsDecide(t *testing.T) {
	t.Parallel()

	model := Model{
		Members: []Membership{
			{Group: "oncall", Kind: "group", ID: "sre"},
			{Group: "sre", Kind: "user", ID: "bob"},
		},
		Grants: []Grant{
			{ID: "1", PrincipalType: "user", PrincipalID: "alice", ResourceType: "Template", ResourceID: "t1", Actions: []string{"own"}},
			{ID: "2", PrincipalType: "group", PrincipalID: "oncall", ResourceType: "Template", ResourceID: "t1", Actions: []string{"read", "update"}},
			{ID: "3", PrincipalType: "idp_group", PrincipalID: "platform", ResourceType: "Route", ResourceID: "*", Actions: []string{"create"}},
			{ID: "4", PrincipalType: "user", PrincipalID: "carol", ResourceType: "Destination", ResourceID: "d1", Actions: []string{"attach"}},
			{ID: "bad", PrincipalType: "user", PrincipalID: "mallory", ResourceType: "Grant", ResourceID: "x", Actions: []string{"read"}},
		},
	}
	a, err := build(1, model)
	if err != nil {
		t.Fatal(err)
	}
	t1, t2 := Resource{Type: "Template", ID: "t1"}, Resource{Type: "Template", ID: "t2"}
	alice, bob := Principal{Subject: "alice"}, Principal{Subject: "bob"}
	dave := Principal{Subject: "dave", IdPGroups: []string{"platform"}}
	tests := []struct {
		name     string
		p        Principal
		action   string
		resource Resource
		want     bool
	}{
		{"the owner reads", alice, ActionRead, t1, true},
		{"the owner updates", alice, ActionUpdate, t1, true},
		{"the owner deletes", alice, ActionDelete, t1, true},
		{"the owner shares", alice, ActionShare, t1, true},
		{"the owner hands ownership on", alice, ActionTransfer, t1, true},
		{"the owner of one is not the owner of another", alice, ActionRead, t2, false},
		{"ownership is not a role", alice, ActionView, Resource{Type: "Template"}, false},
		{"a nested group member reads", bob, ActionRead, t1, true},
		{"a nested group member updates", bob, ActionUpdate, t1, true},
		{"but does not delete", bob, ActionDelete, t1, false},
		{"or share", bob, ActionShare, t1, false},
		{"a provider group creates on the collection", dave, ActionCreate, Resource{Type: "Route"}, true},
		{"but not another type", dave, ActionCreate, Resource{Type: "Template"}, false},
		{"attach is only attach", Principal{Subject: "carol"}, ActionRead, Resource{Type: "Destination", ID: "d1"}, false},
		{"attach", Principal{Subject: "carol"}, ActionAttach, Resource{Type: "Destination", ID: "d1"}, true},
	}
	for _, tt := range tests {
		if got := a.AllowFor(tt.p, tt.action, tt.resource); got != tt.want {
			t.Errorf("%s: %v", tt.name, got)
		}
	}

	if n := len(a.Policies()); n != 4 {
		t.Errorf("%d generated policies, want the 4 that render", n)
	}
	if !strings.Contains(BaseText(), `Role::"admin"`) {
		t.Error("BaseText is not the embedded document")
	}

	holders := []struct {
		name string
		p    Principal
		want bool
	}{
		{"a user grant", alice, true},
		{"through a nested group", bob, true},
		{"through a provider group", dave, true},
		{"nothing", Principal{Subject: "eve", IdPGroups: []string{"other"}}, false},
		{"a refused row counts for nothing", Principal{Subject: "mallory"}, false},
	}
	for _, tt := range holders {
		if got := a.HasGrants(tt.p); got != tt.want {
			t.Errorf("HasGrants(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}

	roleGrant, _ := build(1, Model{Grants: []Grant{{ID: "r", PrincipalType: "role", PrincipalID: "auditor", ResourceType: "Group", ResourceID: "g", Actions: []string{"read"}}}})
	if !roleGrant.HasGrants(Principal{Subject: "x", Roles: []Role{"auditor"}}) {
		t.Error("a role grant does not count")
	}
	empty, _ := build(0, Model{})
	if empty.HasGrants(alice) {
		t.Error("an empty snapshot has grants")
	}
}
