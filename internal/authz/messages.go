package authz

import (
	"slices"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/ast"
	"github.com/cedar-policy/cedar-go/types"
)

// Message actions: naming people in a message (ADR 0082). Each is part of the
// next, so a grant of message covers messageSelf and one of broadcast both.
const (
	// ActionMessageSelf is a message that names only its sender.
	ActionMessageSelf = "messageSelf"
	// ActionMessage is a message that names anyone in the tenant.
	ActionMessage = "message"
	// ActionBroadcast is a message to everyone the bot can reach (ADR 0083).
	ActionBroadcast = "broadcast"
)

// PeopleResource is what the message actions are asked about.
var PeopleResource = Resource{Type: "People", ID: "*"}

// Message levels say how far a principal or a token may address people.
const (
	MessagesNone   = ""
	MessagesSelf   = "self"
	MessagesAnyone = "anyone"
	// MessagesEveryone adds broadcasting to anyone (ADR 0083).
	MessagesEveryone = "everyone"
)

// MessageLevels are the levels a token may be scoped to, weakest first.
var MessageLevels = []string{MessagesSelf, MessagesAnyone, MessagesEveryone}

// messageActions is the action each level stands for.
var messageActions = map[string]string{
	MessagesSelf:     ActionMessageSelf,
	MessagesAnyone:   ActionMessage,
	MessagesEveryone: ActionBroadcast,
}

// MessageAction is the action a level stands for.
func MessageAction(level string) (string, bool) {
	action, ok := messageActions[level]
	return action, ok
}

// MessageLevel is the strongest level the principal holds, or MessagesNone.
func (a *Authorizer) MessageLevel(p Principal) string {
	level := MessagesNone
	for _, candidate := range MessageLevels {
		if a.AllowFor(p, messageActions[candidate], PeopleResource) {
			level = candidate
		}
	}
	return level
}

// LevelCovers reports whether holding limit covers level.
func LevelCovers(limit, level string) bool {
	if level == MessagesNone {
		return true
	}
	held, asked := slices.Index(MessageLevels, limit), slices.Index(MessageLevels, level)
	return held >= 0 && asked >= 0 && asked <= held
}

// renderMessages is the token's message scope as a policy, if it has one.
func (t TokenScope) renderMessages() (*cedar.Policy, bool) {
	action, ok := messageActions[t.Messages]
	if t.ID == "" || !ok {
		return nil, false
	}
	policy := ast.Permit().Annotate("id", types.String("token:"+t.ID+":messages")).
		PrincipalEq(tokenUID(t.ID)).ActionIn(actionUID(action)).ResourceEq(PeopleResource.uid())
	return cedar.NewPolicyFromAST(policy), true
}

// AllowTokenMessage is a scoped token naming people: its own message scope
// must cover the action, and so must its creator's permissions as they are now.
func (a *Authorizer) AllowTokenMessage(tokenID string, creator Principal, action string) bool {
	token := tokenUID(tokenID)
	extra := cedar.EntityMap{token: cedar.Entity{UID: token}}
	decision, _ := cedar.Authorize(a.policies, overlay{base: a.entities, extra: extra}, cedar.Request{
		Principal: token,
		Action:    actionUID(action),
		Resource:  PeopleResource.uid(),
	})
	return decision == cedar.Allow && a.AllowFor(creator, action, PeopleResource)
}
