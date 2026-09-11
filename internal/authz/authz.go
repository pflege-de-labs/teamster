// Package authz answers "may this session do this?" with Cedar, evaluated
// in-process. The policies are a file rather than a chain of ifs so that the
// rules can be read in one place, by someone who does not read Go.
package authz

import (
	_ "embed"
	"fmt"
	"slices"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

//go:embed policies.cedar
var policyDocument []byte

// A Role is what a session may do. They nest: an admin is an editor, an editor
// is a viewer.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
	// RoleNone is a signed-in user the claim said nothing about. It is a role
	// rather than an empty string so that "no permissions" is a state the UI can
	// explain, rather than a session that looks unauthenticated.
	RoleNone Role = "none"
)

// Actions. View is every read, Edit every write; Administer is reserved for the
// grant management a later milestone adds, and today only an admin has it.
const (
	ActionView       = "view"
	ActionEdit       = "edit"
	ActionAdminister = "administer"
)

// A Resource is what an action is attempted on. Type is a Cedar entity type —
// Template, Destination, Route, Directory — and ID is the record, or "*" for
// the collection.
type Resource struct {
	Type string
	ID   string
}

func (r Resource) uid() cedar.EntityUID {
	id := r.ID
	if id == "" {
		id = "*"
	}
	return cedar.NewEntityUID(types.EntityType(r.Type), types.String(id))
}

// An Authorizer holds the parsed policies and the entities they are evaluated
// against. It is immutable, so one is shared by every request.
type Authorizer struct {
	policies *cedar.PolicySet
	entities cedar.EntityMap
}

func New() (*Authorizer, error) {
	policies, err := cedar.NewPolicySetFromBytes("policies.cedar", policyDocument)
	if err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}

	return &Authorizer{policies: policies, entities: roleEntities()}, nil
}

// roleEntities is the role hierarchy: a principal in Role::"admin" is in
// Role::"editor" too, because Cedar's `in` walks the parents.
func roleEntities() cedar.EntityMap {
	admin := cedar.NewEntityUID("Role", types.String(RoleAdmin))
	editor := cedar.NewEntityUID("Role", types.String(RoleEditor))
	viewer := cedar.NewEntityUID("Role", types.String(RoleViewer))

	return cedar.EntityMap{
		admin:  {UID: admin, Parents: cedar.NewEntityUIDSet(editor)},
		editor: {UID: editor, Parents: cedar.NewEntityUIDSet(viewer)},
		viewer: {UID: viewer},
	}
}

// Allow answers whether a subject holding a role may take an action. The
// subject is an entity of its own rather than the role itself, because the
// per-Team grants of the next milestone hang off the principal.
func (a *Authorizer) Allow(subject string, role Role, action string, resource Resource) bool {
	if subject == "" {
		subject = "anonymous"
	}

	principal := cedar.NewEntityUID("User", types.String(subject))
	entities := a.entities.Clone()
	entities[principal] = cedar.Entity{
		UID:     principal,
		Parents: cedar.NewEntityUIDSet(cedar.NewEntityUID("Role", types.String(role))),
	}

	decision, _ := cedar.Authorize(a.policies, entities, cedar.Request{
		Principal: principal,
		Action:    cedar.NewEntityUID("Action", types.String(action)),
		Resource:  resource.uid(),
	})
	return decision == cedar.Allow
}

// RoleFor maps claim values onto a role by name: a provider role called "admin"
// is the admin role here, and nothing has to be configured to say so. The most
// privileged name wins, and a user named by none of them gets the configured
// default — which may be no role at all.
func RoleFor(values []string, defaultRole Role) Role {
	for _, role := range []Role{RoleAdmin, RoleEditor, RoleViewer} {
		if slices.Contains(values, string(role)) {
			return role
		}
	}
	if Valid(defaultRole) {
		return defaultRole
	}
	return RoleNone
}

// Valid reports whether a role is one a user may hold as a grant. RoleNone is
// not one: it is the absence of a grant, not something to configure as a
// default.
func Valid(role Role) bool {
	return role == RoleAdmin || role == RoleEditor || role == RoleViewer
}

// Known reports whether a stored role is one this build understands at all. A
// session written by a later version, or edited by hand, must not be read as
// an admin.
func Known(role Role) bool {
	return Valid(role) || role == RoleNone
}
