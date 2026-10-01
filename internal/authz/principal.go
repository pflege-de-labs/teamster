package authz

import (
	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

// A Principal is who a request acts for: their subject, the roles and the
// identity provider groups their session carries.
type Principal struct {
	Subject   string
	Roles     []Role
	IdPGroups []string
}

// GroupResource is a local group, as an entity a principal can be in.
func GroupResource(id string) Resource { return Resource{Type: "Group", ID: id} }

// IdPGroupResource is a group the identity provider named at sign-in.
func IdPGroupResource(name string) Resource { return Resource{Type: "IdpGroup", ID: name} }

// principalEntity adds the principal to extra. Its parents are its roles, its
// own local groups and its identity provider groups; the snapshot's entities
// carry the rest of each group's ancestry.
func (a *Authorizer) principalEntity(p Principal, extra cedar.EntityMap, attributes cedar.Record) cedar.EntityUID {
	subject := p.Subject
	if subject == "" {
		subject = "anonymous"
	}

	parents := make([]cedar.EntityUID, 0, len(p.Roles)+len(p.IdPGroups))
	for _, role := range p.Roles {
		parents = append(parents, a.ensure(extra, cedar.NewEntityUID("Role", types.String(role))))
	}
	for _, group := range a.userGroups[p.Subject] {
		parents = append(parents, a.ensure(extra, GroupResource(group).uid()))
	}
	for _, name := range p.IdPGroups {
		parents = append(parents, a.ensure(extra, IdPGroupResource(name).uid()))
	}

	principal := cedar.NewEntityUID("User", types.String(subject))
	extra[principal] = cedar.Entity{UID: principal, Parents: cedar.NewEntityUIDSet(parents...), Attributes: attributes}
	return principal
}

// ensure gives a parent the snapshot does not define an entity, or Cedar has
// nothing to resolve it to; a role no policy names still has to exist.
func (a *Authorizer) ensure(extra cedar.EntityMap, uid cedar.EntityUID) cedar.EntityUID {
	if _, known := a.entities[uid]; !known {
		if _, added := extra[uid]; !added {
			extra[uid] = cedar.Entity{UID: uid}
		}
	}
	return uid
}
