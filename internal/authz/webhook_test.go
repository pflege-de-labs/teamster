package authz

import (
	"strings"
	"testing"
)

func TestWebhookGrants(t *testing.T) {
	t.Parallel()

	a, err := build(1, Model{
		Members: []Membership{{Group: "senders", Kind: "user", ID: "carol"}, {Group: "all", Kind: "group", ID: "senders"}},
		Grants: []Grant{
			{ID: "am", PrincipalType: "user", PrincipalID: "alice", ResourceType: "Webhook", ResourceID: WebhookAlertmanager, Actions: []string{ActionUse}},
			{ID: "both", PrincipalType: "group", PrincipalID: "all", ResourceType: "Webhook", ResourceID: WebhooksAll, Actions: []string{ActionUse}},
			{ID: "admin", PrincipalType: "user", PrincipalID: "dave", ResourceType: "Webhook", ResourceID: WebhooksAll, Actions: []string{ActionUse, ActionAdminister}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	am, uni, all := WebhookResource(WebhookAlertmanager), WebhookResource(WebhookUniversal), WebhookResource(WebhooksAll)
	tests := []struct {
		name     string
		p        Principal
		action   string
		resource Resource
		want     bool
	}{
		{"one webhook", Principal{Subject: "alice"}, ActionUse, am, true},
		{"not the other", Principal{Subject: "alice"}, ActionUse, uni, false},
		{"both through a nested group", Principal{Subject: "carol"}, ActionUse, uni, true},
		{"an editor uses both", Principal{Subject: "e", Roles: []Role{RoleEditor}}, ActionUse, am, true},
		{"a viewer uses neither", Principal{Subject: "v", Roles: []Role{RoleViewer}}, ActionUse, am, false},
		{"a webhook admin administers", Principal{Subject: "dave"}, ActionAdminister, all, true},
		{"a sender does not", Principal{Subject: "carol"}, ActionAdminister, all, false},
	}
	for _, tt := range tests {
		if got := a.AllowFor(tt.p, tt.action, tt.resource); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
		}
	}

	refused := []Grant{
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "Webhook", ResourceID: "teamsv2", Actions: []string{ActionUse}},
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "Webhook", ResourceID: WebhookAlertmanager, Actions: []string{ActionAdminister}},
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "Webhook", ResourceID: WebhookAlertmanager, Actions: []string{ActionRead}},
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "Webhook", ResourceID: WebhookAlertmanager},
		{ID: "x", PrincipalType: "user", ResourceType: "Webhook", ResourceID: WebhookAlertmanager, Actions: []string{ActionUse}},
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "Template", ResourceID: "t", Actions: []string{ActionUse}},
	}
	for _, g := range refused {
		if err := g.Validate(); err == nil {
			t.Errorf("Validate(%+v) accepted it", g)
		}
	}
}

func TestExplainAndPoliciesFor(t *testing.T) {
	t.Parallel()

	a, err := build(1, Model{
		Members: []Membership{{Group: "outer", Kind: "group", ID: "inner"}, {Group: "inner", Kind: "idp_group", ID: "platform"}},
		Grants: []Grant{
			{ID: "g1", PrincipalType: "group", PrincipalID: "outer", ResourceType: "Template", ResourceID: "t1", Actions: []string{ActionRead}},
			{ID: "g2", PrincipalType: "user", PrincipalID: "bob", ResourceType: "Template", ResourceID: "t2", Actions: []string{ActionOwn}},
			{ID: "g3", PrincipalType: "role", PrincipalID: "auditor", ResourceType: "Group", ResourceID: "inner", Actions: []string{ActionRead}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{Subject: "eve", Roles: []Role{"auditor"}, IdPGroups: []string{"platform"}}

	if got := a.GroupsOf(p); strings.Join(got, ",") != "inner,outer" {
		t.Errorf("GroupsOf = %v", got)
	}
	var ids []string
	for _, policy := range a.PoliciesFor(p) {
		ids = append(ids, policy.ID)
	}
	if strings.Join(ids, ",") != "perm:g1,perm:g3" {
		t.Errorf("PoliciesFor = %v", ids)
	}

	explained := a.Explain(p, ActionRead, Resource{Type: "Template", ID: "t1"})
	if !explained.Allowed || len(explained.Policies) != 1 || explained.Policies[0].ID != "perm:g1" || !strings.Contains(explained.Policies[0].Text, `Group::"outer"`) {
		t.Errorf("Explain = %+v", explained)
	}
	refused := a.Explain(p, ActionUpdate, Resource{Type: "Template", ID: "t1"})
	if refused.Allowed || len(refused.Policies) != 0 {
		t.Errorf("a refusal explained as %+v", refused)
	}
	byRole := a.Explain(Principal{Subject: "v", Roles: []Role{RoleViewer}}, ActionView, Resource{Type: "Template"})
	if !byRole.Allowed || len(byRole.Policies) == 0 || !strings.Contains(byRole.Policies[0].Text, `Role::"viewer"`) {
		t.Errorf("a role decision explained as %+v", byRole)
	}
}
