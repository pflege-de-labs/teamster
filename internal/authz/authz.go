// Package authz answers "may this session do this?" with Cedar, evaluated
// in-process. The policies are a file rather than a chain of ifs so that the
// rules can be read in one place, by someone who does not read Go.
package authz

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

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

// Allow answers whether a subject holding these roles may take an action. Every
// role the provider named is a parent of the principal, including ones this
// build has never heard of: a deployment that defines its own client role and
// writes a policy for it gets that policy applied. A role no policy mentions
// grants nothing, which is what makes passing them all through safe.
//
// The subject is an entity of its own rather than the role itself, because the
// per-Team grants of the next milestone hang off the principal.
func (a *Authorizer) Allow(subject string, roles []Role, action string, resource Resource) bool {
	if subject == "" {
		subject = "anonymous"
	}

	entities := a.entities.Clone()
	parents := make([]cedar.EntityUID, 0, len(roles))
	for _, role := range roles {
		uid := cedar.NewEntityUID("Role", types.String(role))
		parents = append(parents, uid)
		// A role the embedded policies do not define still needs an entity, or
		// Cedar has nothing to resolve the parent to.
		if _, known := entities[uid]; !known {
			entities[uid] = cedar.Entity{UID: uid}
		}
	}

	principal := cedar.NewEntityUID("User", types.String(subject))
	entities[principal] = cedar.Entity{
		UID:     principal,
		Parents: cedar.NewEntityUIDSet(parents...),
	}

	decision, _ := cedar.Authorize(a.policies, entities, cedar.Request{
		Principal: principal,
		Action:    cedar.NewEntityUID("Action", types.String(action)),
		Resource:  resource.uid(),
	})
	return decision == cedar.Allow
}

// RolesFor maps claim values onto roles one to one: a provider role called
// "admin" is the admin role here, and one called "auditor" is an auditor role
// that whatever policy mentions it decides about. Nothing has to be configured
// to say so.
//
// A user the claim names nothing for gets the configured default, which may be
// no role at all.
func RolesFor(values []string, defaultRole Role) []Role {
	roles := make([]Role, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == string(RoleNone) {
			continue
		}
		if !slices.Contains(roles, Role(value)) {
			roles = append(roles, Role(value))
		}
	}

	// The default fills in for a missing *Teamster* role, not for an empty claim:
	// a Keycloak realm hands out offline_access to everyone, so "carries no
	// values at all" would almost never be true and the default would never
	// apply. Roles the deployment defined for itself are kept either way.
	if !HasBuiltin(roles) && Valid(defaultRole) {
		roles = append(roles, defaultRole)
	}
	return roles
}

// HasBuiltin reports whether any of the roles is one this build defines
// policies for. Without one, a user holds only roles a deployment has to have
// written its own policies for — and possibly none at all.
func HasBuiltin(roles []Role) bool {
	for _, role := range roles {
		if Valid(role) {
			return true
		}
	}
	return false
}

// Encode renders roles for storage in a session row, and Decode reads them
// back. A stored session says what its holder may do, so a role granted or
// taken away at the provider applies at the next sign-in rather than mid-session.
func Encode(roles []Role) string {
	if len(roles) == 0 {
		return string(RoleNone)
	}

	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, string(role))
	}
	return strings.Join(names, " ")
}

func Decode(stored string) []Role {
	var roles []Role
	for _, name := range strings.Fields(stored) {
		if name == string(RoleNone) {
			continue
		}
		roles = append(roles, Role(name))
	}
	return roles
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
