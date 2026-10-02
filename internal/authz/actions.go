package authz

import (
	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

// Record actions (ADR 0073). Each is a child of the coarse actions the role
// policies name, so editors keep what they had and owners get them through own.
const (
	ActionRead     = "read"
	ActionCreate   = "create"
	ActionUpdate   = "update"
	ActionDelete   = "delete"
	ActionAttach   = "attach"
	ActionShare    = "share"
	ActionTransfer = "transfer"
	// ActionOwn is the group an owner holds; granting it makes an owner.
	ActionOwn = "own"
	// ActionUse is sending to a webhook, as a token's creator must be allowed to (ADR 0076).
	ActionUse = "use"
)

// The webhooks, as Cedar resources; WebhooksAll is both of them.
const (
	WebhookAlertmanager = "alertmanager"
	WebhookUniversal    = "universal"
	WebhooksAll         = "*"
)

// WebhookResource is one webhook, or "*" for both.
func WebhookResource(name string) Resource { return Resource{Type: "Webhook", ID: name} }

// addWebhookEntities makes each webhook a member of the "*" that stands for both.
func addWebhookEntities(entities cedar.EntityMap) {
	all := WebhookResource(WebhooksAll).uid()
	entities[all] = cedar.Entity{UID: all}
	for _, name := range []string{WebhookAlertmanager, WebhookUniversal} {
		uid := WebhookResource(name).uid()
		entities[uid] = cedar.Entity{UID: uid, Parents: cedar.NewEntityUIDSet(all)}
	}
}

// actionParents is the action hierarchy: `action in Action::"edit"` holds for update.
var actionParents = map[string][]string{
	ActionRead:     {ActionView, ActionOwn},
	ActionCreate:   {ActionEdit},
	ActionUpdate:   {ActionEdit, ActionOwn},
	ActionDelete:   {ActionEdit, ActionOwn},
	ActionAttach:   {ActionEdit, ActionOwn},
	ActionShare:    {ActionOwn},
	ActionTransfer: {ActionOwn},
	// A grant of message covers messages to oneself (ADR 0082).
	ActionMessageSelf: {ActionMessage},
}

func actionUID(action string) cedar.EntityUID {
	return cedar.NewEntityUID("Action", types.String(action))
}

func addActionEntities(entities cedar.EntityMap) {
	for action, parents := range actionParents {
		uids := make([]cedar.EntityUID, 0, len(parents))
		for _, parent := range parents {
			uids = append(uids, actionUID(parent))
		}
		entities[actionUID(action)] = cedar.Entity{UID: actionUID(action), Parents: cedar.NewEntityUIDSet(uids...)}
	}
	for _, parents := range actionParents {
		for _, group := range parents {
			if _, ok := entities[actionUID(group)]; !ok {
				entities[actionUID(group)] = cedar.Entity{UID: actionUID(group)}
			}
		}
	}
}
