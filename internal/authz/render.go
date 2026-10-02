package authz

import (
	"fmt"
	"slices"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/ast"
	"github.com/cedar-policy/cedar-go/types"
)

// A Grant is one permission row: the actions a principal holds on a resource (ADR 0075).
type Grant struct {
	ID            string
	PrincipalType string
	PrincipalID   string
	ResourceType  string
	ResourceID    string
	Actions       []string
}

// PermissionedTypes are the resources a grant may name.
var PermissionedTypes = []string{"Template", "Destination", "Route", "WebhookEndpoint", "Group"}

// GrantableActions are the actions a grant may hold. own holds the rest.
var GrantableActions = []string{ActionRead, ActionCreate, ActionUpdate, ActionDelete, ActionAttach, ActionShare, ActionOwn}

// A GeneratedPolicy is a grant as the Cedar text the snapshot evaluates.
type GeneratedPolicy struct {
	ID    string
	Grant Grant
	Text  string
}

// Validate refuses a grant that would not render, or would render to something
// other than what it says.
func (g Grant) Validate() error {
	if g.ResourceType == "Webhook" {
		return g.validateWebhook()
	}
	if g.ResourceType == PeopleResource.Type {
		return g.validatePeople()
	}
	if g.PrincipalID == "" || g.ResourceID == "" {
		return fmt.Errorf("a grant needs a principal and a resource")
	}
	switch g.PrincipalType {
	case "user", "group", "idp_group", "role":
	default:
		return fmt.Errorf("unknown principal type %q", g.PrincipalType)
	}
	if !slices.Contains(PermissionedTypes, g.ResourceType) {
		return fmt.Errorf("permissions cannot be granted on %q", g.ResourceType)
	}
	if len(g.Actions) == 0 {
		return fmt.Errorf("a grant needs at least one action")
	}
	for _, action := range g.Actions {
		if !slices.Contains(GrantableActions, action) {
			return fmt.Errorf("unknown action %q", action)
		}
		// Creating is asked of the collection, everything else of a record.
		if (action == ActionCreate) != (g.ResourceID == "*") {
			return fmt.Errorf("create is granted on the collection, and only create is")
		}
	}
	return nil
}

// validateWebhook allows use on one webhook or both, and administer on both.
func (g Grant) validateWebhook() error {
	if g.PrincipalID == "" || !slices.Contains([]string{"user", "group", "idp_group", "role"}, g.PrincipalType) {
		return fmt.Errorf("a grant needs a principal")
	}
	if !slices.Contains([]string{WebhookAlertmanager, WebhookUniversal, WebhooksAll}, g.ResourceID) {
		return fmt.Errorf("unknown webhook %q", g.ResourceID)
	}
	if len(g.Actions) == 0 {
		return fmt.Errorf("a grant needs at least one action")
	}
	for _, action := range g.Actions {
		switch {
		case action == ActionUse:
		case action == ActionAdminister && g.ResourceID == WebhooksAll:
		default:
			return fmt.Errorf("a webhook grant holds use, and administer on both webhooks, not %q", action)
		}
	}
	return nil
}

// validatePeople allows message on everyone; messages to oneself need no grant.
func (g Grant) validatePeople() error {
	if g.PrincipalID == "" || !slices.Contains([]string{"user", "group", "idp_group", "role"}, g.PrincipalType) {
		return fmt.Errorf("a grant needs a principal")
	}
	if g.ResourceID != PeopleResource.ID {
		return fmt.Errorf("a message grant is on People %q, not %q", PeopleResource.ID, g.ResourceID)
	}
	if len(g.Actions) == 0 {
		return fmt.Errorf("a grant needs at least one action")
	}
	for _, action := range g.Actions {
		if action != ActionMessage {
			return fmt.Errorf("a message grant holds message, not %q", action)
		}
	}
	return nil
}

// Render builds the grant's policy from the AST, never from text, so no id can
// smuggle Cedar syntax into it.
func (g Grant) Render() (*cedar.Policy, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	policy := ast.Permit().Annotate("id", types.String("perm:"+g.ID))
	switch g.PrincipalType {
	case "user":
		policy = policy.PrincipalEq(cedar.NewEntityUID("User", types.String(g.PrincipalID)))
	case "group":
		policy = policy.PrincipalIn(GroupResource(g.PrincipalID).uid())
	case "idp_group":
		policy = policy.PrincipalIn(IdPGroupResource(g.PrincipalID).uid())
	case "role":
		policy = policy.PrincipalIn(cedar.NewEntityUID("Role", types.String(g.PrincipalID)))
	}
	actions := make([]types.EntityUID, 0, len(g.Actions))
	for _, action := range g.Actions {
		actions = append(actions, actionUID(action))
	}
	policy = policy.ActionInSet(actions...)
	resource := Resource{Type: g.ResourceType, ID: g.ResourceID}.uid()
	if g.ResourceType == "Webhook" && g.ResourceID == WebhooksAll {
		// Both webhooks are in "*", so a grant on it reaches each.
		policy = policy.ResourceIn(resource)
	} else {
		policy = policy.ResourceEq(resource)
	}
	return cedar.NewPolicyFromAST(policy), nil
}

// principalKey indexes grants by who they name.
func principalKey(kind, id string) string { return kind + "\x00" + id }

// addGrants renders the grants into policies. A grant that does not render is
// left out rather than failing the snapshot: writes validate, so it is a row
// edited by hand, and refusing everyone over it would be worse.
func addGrants(policies *cedar.PolicySet, grants []Grant) ([]GeneratedPolicy, map[string]bool) {
	generated := make([]GeneratedPolicy, 0, len(grants))
	holders := map[string]bool{}
	for _, g := range grants {
		policy, err := g.Render()
		if err != nil {
			continue
		}
		id := "perm:" + g.ID
		policies.Add(cedar.PolicyID(id), policy)
		generated = append(generated, GeneratedPolicy{ID: id, Grant: g, Text: string(policy.MarshalCedar())})
		holders[principalKey(g.PrincipalType, g.PrincipalID)] = true
	}
	return generated, holders
}

// Policies are the generated policies this snapshot evaluates, as text.
func (a *Authorizer) Policies() []GeneratedPolicy { return a.generated }

// BaseText is the embedded policy document.
func BaseText() string { return string(policyDocument) }

// HasGrants reports whether any grant names the principal, directly or
// through a role or a group however deep. It is what lets someone with no
// role reach the pages that list what was shared with them.
func (a *Authorizer) HasGrants(p Principal) bool {
	if len(a.holders) == 0 {
		return false
	}
	if a.holders[principalKey("user", p.Subject)] {
		return true
	}
	for _, role := range p.Roles {
		if a.holders[principalKey("role", string(role))] {
			return true
		}
	}
	start := make([]cedar.EntityUID, 0, len(p.IdPGroups)+len(a.userGroups[p.Subject]))
	for _, name := range p.IdPGroups {
		if a.holders[principalKey("idp_group", name)] {
			return true
		}
		start = append(start, IdPGroupResource(name).uid())
	}
	for _, group := range a.userGroups[p.Subject] {
		start = append(start, GroupResource(group).uid())
	}
	seen := map[cedar.EntityUID]bool{}
	for len(start) > 0 {
		uid := start[len(start)-1]
		start = start[:len(start)-1]
		if seen[uid] {
			continue
		}
		seen[uid] = true
		if uid.Type == "Group" && a.holders[principalKey("group", string(uid.ID))] {
			return true
		}
		if entity, ok := a.entities[uid]; ok {
			for parent := range entity.Parents.All() {
				start = append(start, parent)
			}
		}
	}
	return false
}

// An Explanation is a decision and the policies that made it.
type Explanation struct {
	Allowed  bool
	Policies []ExplainedPolicy
}

// ExplainedPolicy is one deciding policy, by id and as text.
type ExplainedPolicy struct {
	ID   string
	Text string
}

// Explain is AllowFor, with the policies that decided it.
func (a *Authorizer) Explain(p Principal, action string, resource Resource) Explanation {
	extra := cedar.EntityMap{}
	principal := a.principalEntity(p, extra, cedar.Record{})
	decision, diagnostic := cedar.Authorize(a.policies, overlay{base: a.entities, extra: extra}, cedar.Request{
		Principal: principal,
		Action:    actionUID(action),
		Resource:  resource.uid(),
	})
	out := Explanation{Allowed: decision == cedar.Allow}
	for _, reason := range diagnostic.Reasons {
		text := ""
		if policy := a.policies.Get(reason.PolicyID); policy != nil {
			text = string(policy.MarshalCedar())
		}
		out.Policies = append(out.Policies, ExplainedPolicy{ID: string(reason.PolicyID), Text: text})
	}
	return out
}

// GroupsOf is every local group the principal is in, however deep.
func (a *Authorizer) GroupsOf(p Principal) []string {
	start := make([]cedar.EntityUID, 0, len(p.IdPGroups)+len(a.userGroups[p.Subject]))
	for _, name := range p.IdPGroups {
		start = append(start, IdPGroupResource(name).uid())
	}
	for _, group := range a.userGroups[p.Subject] {
		start = append(start, GroupResource(group).uid())
	}
	seen := map[cedar.EntityUID]bool{}
	var groups []string
	for len(start) > 0 {
		uid := start[len(start)-1]
		start = start[:len(start)-1]
		if seen[uid] {
			continue
		}
		seen[uid] = true
		if uid.Type == "Group" {
			groups = append(groups, string(uid.ID))
		}
		if entity, ok := a.entities[uid]; ok {
			for parent := range entity.Parents.All() {
				start = append(start, parent)
			}
		}
	}
	slices.Sort(groups)
	return groups
}

// PoliciesFor are the generated policies that name the principal, through
// its subject, a role, a provider group or a local group.
func (a *Authorizer) PoliciesFor(p Principal) []GeneratedPolicy {
	groups := a.GroupsOf(p)
	var out []GeneratedPolicy
	for _, policy := range a.generated {
		g := policy.Grant
		switch {
		case g.PrincipalType == "user" && g.PrincipalID == p.Subject,
			g.PrincipalType == "role" && slices.Contains(p.Roles, Role(g.PrincipalID)),
			g.PrincipalType == "idp_group" && slices.Contains(p.IdPGroups, g.PrincipalID),
			g.PrincipalType == "group" && slices.Contains(groups, g.PrincipalID):
			out = append(out, policy)
		}
	}
	return out
}
