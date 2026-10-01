package authz

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	cedar "github.com/cedar-policy/cedar-go"
)

// A Model is what the store adds to the embedded policies; the generation says
// when it changed.
type Model struct {
	// Members are every group membership (ADR 0074).
	Members []Membership
	// Grants are every permission row (ADR 0075).
	Grants []Grant
}

// A Membership puts a user, a group or an identity provider group in Group.
type Membership struct {
	Group string
	// Kind is "user", "group" or "idp_group", as the store names them.
	Kind string
	ID   string
}

// A Source tells the engine when the model changed and what it is (ADR 0073).
type Source interface {
	// Generation is bumped in the same transaction as every change to the model.
	Generation(ctx context.Context) (int64, error)
	Load(ctx context.Context) (Model, error)
}

// An Engine hands out the current Authorizer. One generation read per request
// keeps every replica current without a cache to expire or a bus to listen to.
type Engine struct {
	src     Source
	current atomic.Pointer[Authorizer]
	// mu makes concurrent requests that see a new generation build it once.
	mu sync.Mutex
}

// NewEngine fails when the embedded policies do not parse. A nil Source is a
// model that never changes, for tests and for tools without a store.
func NewEngine(src Source) (*Engine, error) {
	base, err := build(0, Model{})
	if err != nil {
		return nil, err
	}
	e := &Engine{src: src}
	e.current.Store(base)
	return e, nil
}

// Base is the last snapshot built, without asking the source.
func (e *Engine) Base() *Authorizer { return e.current.Load() }

// Authorizer returns the snapshot for the source's current generation,
// rebuilding it first when the model changed since the last one.
func (e *Engine) Authorizer(ctx context.Context) (*Authorizer, error) {
	if e.src == nil {
		return e.current.Load(), nil
	}
	generation, err := e.src.Generation(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz generation: %w", err)
	}
	if current := e.current.Load(); current.generation == generation {
		return current, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if current := e.current.Load(); current.generation == generation {
		return current, nil
	}
	model, err := e.src.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load authz model: %w", err)
	}
	next, err := build(generation, model)
	if err != nil {
		return nil, err
	}
	e.current.Store(next)
	return next, nil
}

func build(generation int64, model Model) (*Authorizer, error) {
	policies, err := cedar.NewPolicySetFromBytes("policies.cedar", policyDocument)
	if err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}
	entities := roleEntities()
	addActionEntities(entities)
	addWebhookEntities(entities)
	userGroups := addGroupEntities(entities, model.Members)
	generated, holders := addGrants(policies, model.Grants)
	return &Authorizer{
		policies: policies, entities: entities, generation: generation,
		userGroups: userGroups, generated: generated, holders: holders,
	}, nil
}

// addGroupEntities makes each group and identity provider group an entity
// whose parents are the groups it is a member of, so `in Group::"x"` walks
// nesting. Users get their groups per request; this returns them by subject.
func addGroupEntities(entities cedar.EntityMap, members []Membership) map[string][]string {
	parents := map[cedar.EntityUID][]cedar.EntityUID{}
	userGroups := map[string][]string{}
	for _, m := range members {
		group := GroupResource(m.Group).uid()
		if _, ok := parents[group]; !ok {
			parents[group] = nil
		}
		switch m.Kind {
		case "user":
			userGroups[m.ID] = append(userGroups[m.ID], m.Group)
		case "group":
			child := GroupResource(m.ID).uid()
			parents[child] = append(parents[child], group)
		case "idp_group":
			child := IdPGroupResource(m.ID).uid()
			parents[child] = append(parents[child], group)
		}
	}
	for uid, of := range parents {
		entities[uid] = cedar.Entity{UID: uid, Parents: cedar.NewEntityUIDSet(of...)}
	}
	return userGroups
}

// overlay adds a request's own entities to a snapshot's without copying it.
type overlay struct {
	base  cedar.EntityMap
	extra cedar.EntityMap
}

func (o overlay) Get(uid cedar.EntityUID) (cedar.Entity, bool) {
	if entity, ok := o.extra[uid]; ok {
		return entity, true
	}
	entity, ok := o.base[uid]
	return entity, ok
}
