package authz

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	cedar "github.com/cedar-policy/cedar-go"
)

// A Model is what the store adds to the embedded policies. It is empty until
// groups and permissions exist; the generation says when it changed.
type Model struct{}

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

func build(generation int64, _ Model) (*Authorizer, error) {
	policies, err := cedar.NewPolicySetFromBytes("policies.cedar", policyDocument)
	if err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}
	entities := roleEntities()
	addActionEntities(entities)
	return &Authorizer{policies: policies, entities: entities, generation: generation}, nil
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
