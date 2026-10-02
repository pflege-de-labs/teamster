package authz

import "testing"

func TestMessageGrants(t *testing.T) {
	t.Parallel()

	a, err := build(1, Model{
		Members: []Membership{{Group: "office", Kind: "idp_group", ID: "office-admins"}},
		Grants: []Grant{
			{ID: "m", PrincipalType: "group", PrincipalID: "office", ResourceType: PeopleResource.Type, ResourceID: PeopleResource.ID, Actions: []string{ActionMessage}},
			{ID: "b", PrincipalType: "user", PrincipalID: "bea", ResourceType: PeopleResource.Type, ResourceID: PeopleResource.ID, Actions: []string{ActionBroadcast}},
		},
		Tokens: []TokenScope{
			{ID: "self", Webhooks: []string{WebhookUniversal}, Messages: MessagesSelf},
			{ID: "anyone", Webhooks: []string{WebhookUniversal}, Messages: MessagesAnyone},
			{ID: "everyone", Webhooks: []string{WebhookUniversal}, Messages: MessagesEveryone},
			{ID: "silent", Webhooks: []string{WebhookUniversal}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	office := Principal{Subject: "olga", IdPGroups: []string{"office-admins"}}
	editor := Principal{Subject: "ed", Roles: []Role{RoleEditor}}
	admin := Principal{Subject: "ada", Roles: []Role{RoleAdmin}}
	broadcaster := Principal{Subject: "bea"}

	levels := []struct {
		name string
		p    Principal
		want string
	}{
		{"anyone signed in messages themselves", Principal{Subject: "nobody"}, MessagesSelf},
		{"an editor is no exception", editor, MessagesSelf},
		{"a grant through a provider group", office, MessagesAnyone},
		{"an admin through the admin policy", admin, MessagesEveryone},
		{"a broadcast grant includes naming anyone", broadcaster, MessagesEveryone},
	}
	for _, tt := range levels {
		if got := a.MessageLevel(tt.p); got != tt.want {
			t.Errorf("%s: MessageLevel = %q, want %q", tt.name, got, tt.want)
		}
	}

	tokens := []struct {
		name    string
		token   string
		creator Principal
		action  string
		want    bool
	}{
		{"a self token for its creator", "self", editor, ActionMessageSelf, true},
		{"a self token names nobody else", "self", office, ActionMessage, false},
		{"an anyone token covers self", "anyone", office, ActionMessageSelf, true},
		{"an anyone token of a granted creator", "anyone", office, ActionMessage, true},
		{"an anyone token outlives no grant", "anyone", editor, ActionMessage, false},
		{"a token without a message scope", "silent", admin, ActionMessageSelf, false},
		{"an everyone token broadcasts", "everyone", broadcaster, ActionBroadcast, true},
		{"an everyone token names anyone", "everyone", broadcaster, ActionMessage, true},
		{"an anyone token does not broadcast", "anyone", admin, ActionBroadcast, false},
		{"an everyone token of a creator who may not", "everyone", office, ActionBroadcast, false},
		{"an unknown token", "gone", admin, ActionMessageSelf, false},
	}
	for _, tt := range tokens {
		if got := a.AllowTokenMessage(tt.token, tt.creator, tt.action); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
		}
	}

	refused := []Grant{
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "People", ResourceID: "someone", Actions: []string{ActionMessage}},
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "People", ResourceID: "*", Actions: []string{ActionMessageSelf}},
		{ID: "x", PrincipalType: "user", PrincipalID: "a", ResourceType: "People", ResourceID: "*"},
		{ID: "x", PrincipalType: "user", ResourceType: "People", ResourceID: "*", Actions: []string{ActionMessage}},
	}
	for _, g := range refused {
		if err := g.Validate(); err == nil {
			t.Errorf("Validate(%+v) accepted it", g)
		}
	}
}

func TestLevelCovers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		limit, level string
		want         bool
	}{
		{MessagesNone, MessagesNone, true},
		{MessagesNone, MessagesSelf, false},
		{MessagesSelf, MessagesSelf, true},
		{MessagesSelf, MessagesAnyone, false},
		{MessagesAnyone, MessagesSelf, true},
		{MessagesAnyone, MessagesEveryone, false},
		{MessagesEveryone, MessagesAnyone, true},
		{MessagesAnyone, "everyone-ish", false},
	}
	for _, tt := range tests {
		if got := LevelCovers(tt.limit, tt.level); got != tt.want {
			t.Errorf("LevelCovers(%q, %q) = %v, want %v", tt.limit, tt.level, got, tt.want)
		}
	}
}
