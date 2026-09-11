package authz

import (
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// Scoped actions. Delivering to a channel and seeing one are separate from the
// blanket view and edit, so that a policy can narrow them without narrowing
// everything else a role may do.
const (
	ActionDeliver     = "deliver"
	ActionViewChannel = "viewChannel"
	ActionViewTeam    = "viewTeam"
)

// A Scope is what the grants say a principal reaches. Unrestricted is not the
// same as an empty scope: no grant naming any of a user's roles leaves them
// unrestricted, which is how an installation behaves before an admin narrows
// anything, while a grant that names their role and nothing they are asking
// about refuses.
type Scope struct {
	Unrestricted bool
	Channels     []Resource
	Teams        []Resource
}

// ScopeFor collects the grants that name any of these roles. A grant on a Team
// covers the channels in it, which Cedar resolves through the channel's parent
// rather than by expanding the Team here — the channel list is not known at this
// point, and asking Graph for it to answer a permission question would make
// every check depend on a network call.
func ScopeFor(roles []Role, grants []models.Grant) Scope {
	named := map[string]bool{}
	for _, role := range roles {
		named[string(role)] = true
	}

	var (
		scope   Scope
		limited bool
		teams   = map[string]bool{}
	)
	for _, grant := range grants {
		if !named[grant.Role] {
			continue
		}
		limited = true

		if !teams[grant.TeamID] {
			teams[grant.TeamID] = true
			scope.Teams = append(scope.Teams, TeamResource(grant.TeamID))
		}
		if grant.ChannelID == "" {
			scope.Channels = append(scope.Channels, TeamResource(grant.TeamID))
			continue
		}
		scope.Channels = append(scope.Channels, ChannelResource(grant.TeamID, grant.ChannelID))
	}

	scope.Unrestricted = !limited
	return scope
}

func TeamResource(teamID string) Resource {
	return Resource{Type: "Team", ID: teamID}
}

// channelSeparator joins a Team to a channel in an entity id. It is a control
// character rather than a slash because the two ids are typed by hand in the
// admin UI: a Team id containing the separator would otherwise split in the
// wrong place, and a crafted one could resolve a channel to a Team that was
// granted rather than the one it is in.
const channelSeparator = "\x1f"

// A channel is identified by its Team as well, because channel ids are only
// unique within one.
func ChannelResource(teamID, channelID string) Resource {
	return Resource{Type: "Channel", ID: teamID + channelSeparator + channelID}
}

// SafeID reports whether an id can be used in an entity without ambiguity. The
// separator is the only character that matters, and no Microsoft Graph id
// contains it.
func SafeID(id string) bool {
	return !strings.Contains(id, channelSeparator)
}

// AllowScoped answers a question the grants bear on: may this principal deliver
// to this channel, see it, or see this Team. The entity graph is built per
// request because it is made of that request's roles and grants.
func (a *Authorizer) AllowScoped(subject string, roles []Role, action string, resource Resource, scope Scope) bool {
	if subject == "" {
		subject = "anonymous"
	}

	entities := a.entities.Clone()
	parents := make([]cedar.EntityUID, 0, len(roles))
	for _, role := range roles {
		uid := cedar.NewEntityUID("Role", types.String(role))
		parents = append(parents, uid)
		if _, known := entities[uid]; !known {
			entities[uid] = cedar.Entity{UID: uid}
		}
	}

	// The resource and everything in the scope have to exist as entities, or
	// Cedar has nothing for `in` to walk.
	addResource(entities, resource)
	scopes := make([]types.Value, 0, len(scope.Channels))
	for _, granted := range scope.Channels {
		addResource(entities, granted)
		scopes = append(scopes, granted.uid())
	}
	teams := make([]types.Value, 0, len(scope.Teams))
	for _, granted := range scope.Teams {
		addResource(entities, granted)
		teams = append(teams, granted.uid())
	}

	principal := cedar.NewEntityUID("User", types.String(subject))
	entities[principal] = cedar.Entity{
		UID:     principal,
		Parents: cedar.NewEntityUIDSet(parents...),
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"unrestricted": types.Boolean(scope.Unrestricted),
			"scopes":       cedar.NewSet(scopes...),
			"teams":        cedar.NewSet(teams...),
		}),
	}

	decision, _ := cedar.Authorize(a.policies, entities, cedar.Request{
		Principal: principal,
		Action:    cedar.NewEntityUID("Action", types.String(action)),
		Resource:  resource.uid(),
	})
	return decision == cedar.Allow
}

// addResource registers an entity, giving a channel its Team as a parent so
// that a grant on the Team reaches it.
func addResource(entities cedar.EntityMap, resource Resource) {
	uid := resource.uid()
	if _, known := entities[uid]; known {
		return
	}

	entity := cedar.Entity{UID: uid}
	if resource.Type == "Channel" {
		if teamID, _, found := strings.Cut(resource.ID, channelSeparator); found {
			team := TeamResource(teamID)
			addResource(entities, team)
			entity.Parents = cedar.NewEntityUIDSet(team.uid())
		}
	}
	entities[uid] = entity
}
