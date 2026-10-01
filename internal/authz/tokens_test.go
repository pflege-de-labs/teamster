package authz

import (
	"strings"
	"testing"
)

func TestAllowToken(t *testing.T) {
	t.Parallel()

	a, err := build(1, Model{
		Grants: []Grant{{ID: "g", PrincipalType: "user", PrincipalID: "viewer-with-grant", ResourceType: "Webhook", ResourceID: WebhookAlertmanager, Actions: []string{ActionUse}}},
		Tokens: []TokenScope{
			{ID: "am", Webhooks: []string{WebhookAlertmanager}},
			{ID: "both", Webhooks: []string{WebhookAlertmanager, WebhookUniversal}},
			{ID: "star", Webhooks: []string{WebhooksAll}},
			{ID: "bad", Webhooks: []string{"teamsv2"}},
			{ID: "empty"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	editor := Principal{Subject: "e", Roles: []Role{RoleEditor}}
	viewer := Principal{Subject: "v", Roles: []Role{RoleViewer}}
	granted := Principal{Subject: "viewer-with-grant", Roles: []Role{RoleViewer}}
	tests := []struct {
		name    string
		token   string
		creator Principal
		webhook string
		want    bool
	}{
		{"in scope, creator may", "am", editor, WebhookAlertmanager, true},
		{"out of scope", "am", editor, WebhookUniversal, false},
		{"both named", "both", editor, WebhookUniversal, true},
		{"star", "star", editor, WebhookUniversal, true},
		{"the creator may not", "star", viewer, WebhookAlertmanager, false},
		{"the creator's grant", "star", granted, WebhookAlertmanager, true},
		{"beyond the creator's grant", "star", granted, WebhookUniversal, false},
		{"a token that did not render", "bad", editor, WebhookAlertmanager, false},
		{"an unknown token", "nope", editor, WebhookAlertmanager, false},
		{"an empty scope", "empty", editor, WebhookAlertmanager, false},
	}
	for _, tt := range tests {
		if got := a.AllowToken(tt.token, tt.creator, tt.webhook); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
		}
	}

	// A token is not a principal any other policy reaches.
	if a.AllowFor(Principal{Subject: "am"}, ActionUse, WebhookResource(WebhookAlertmanager)) {
		t.Error("a user named like a token got its scope")
	}
	var texts []string
	for _, p := range a.Policies() {
		if strings.HasPrefix(p.ID, "token:") {
			texts = append(texts, p.Text)
		}
	}
	if len(texts) != 3 || !strings.Contains(texts[0], `principal == Token::"am"`) {
		t.Errorf("token policies = %v", texts)
	}
}
