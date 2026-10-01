package authz

import (
	"slices"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/ast"
	"github.com/cedar-policy/cedar-go/types"
)

// A TokenScope is a scoped access token and the webhooks it names (ADR 0077).
type TokenScope struct {
	ID       string
	Webhooks []string
}

func tokenUID(id string) cedar.EntityUID { return cedar.NewEntityUID("Token", types.String(id)) }

// Render is the token's scope as a policy: the token, and nothing else, may use these webhooks.
func (t TokenScope) Render() (*cedar.Policy, bool) {
	if t.ID == "" || len(t.Webhooks) == 0 {
		return nil, false
	}
	policy := ast.Permit().Annotate("id", types.String("token:"+t.ID)).
		PrincipalEq(tokenUID(t.ID)).ActionEq(actionUID(ActionUse))
	if slices.Contains(t.Webhooks, WebhooksAll) {
		return cedar.NewPolicyFromAST(policy.ResourceIn(WebhookResource(WebhooksAll).uid())), true
	}
	webhooks := make([]types.EntityUID, 0, len(t.Webhooks))
	for _, name := range t.Webhooks {
		if name != WebhookAlertmanager && name != WebhookUniversal {
			return nil, false
		}
		webhooks = append(webhooks, WebhookResource(name).uid())
	}
	if len(webhooks) == 1 {
		return cedar.NewPolicyFromAST(policy.ResourceEq(webhooks[0])), true
	}
	// Both named one by one is both.
	return cedar.NewPolicyFromAST(policy.ResourceIn(WebhookResource(WebhooksAll).uid())), true
}

func addTokens(policies *cedar.PolicySet, tokens []TokenScope) []GeneratedPolicy {
	out := make([]GeneratedPolicy, 0, len(tokens))
	for _, token := range tokens {
		policy, ok := token.Render()
		if !ok {
			continue
		}
		id := "token:" + token.ID
		policies.Add(cedar.PolicyID(id), policy)
		out = append(out, GeneratedPolicy{ID: id, Text: string(policy.MarshalCedar())})
	}
	return out
}

// AllowToken is the use of a scoped token: its own scope must name the
// webhook, and its creator, as they are now, must be allowed to use it.
// Either alone is not enough, so revoking the creator revokes the token.
func (a *Authorizer) AllowToken(tokenID string, creator Principal, webhook string) bool {
	resource := WebhookResource(webhook)
	extra := cedar.EntityMap{}
	token := tokenUID(tokenID)
	extra[token] = cedar.Entity{UID: token}
	decision, _ := cedar.Authorize(a.policies, overlay{base: a.entities, extra: extra}, cedar.Request{
		Principal: token,
		Action:    actionUID(ActionUse),
		Resource:  resource.uid(),
	})
	return decision == cedar.Allow && a.AllowFor(creator, ActionUse, resource)
}
