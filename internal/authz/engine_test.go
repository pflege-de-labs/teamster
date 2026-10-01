package authz

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type countingSource struct {
	generation atomic.Int64
	loads      atomic.Int64
	genErr     error
	loadErr    error
}

func (s *countingSource) Generation(context.Context) (int64, error) {
	return s.generation.Load(), s.genErr
}

func (s *countingSource) Load(context.Context) (Model, error) {
	s.loads.Add(1)
	return Model{}, s.loadErr
}

func TestEngineRebuildsOnlyWhenTheGenerationMoves(t *testing.T) {
	t.Parallel()

	src := &countingSource{}
	e, err := NewEngine(src)
	if err != nil {
		t.Fatal(err)
	}

	first, err := e.Authorizer(t.Context())
	if err != nil || first.Generation() != 0 || src.loads.Load() != 0 {
		t.Fatalf("generation 0: %v, gen %d, loads %d", err, first.Generation(), src.loads.Load())
	}

	src.generation.Store(3)
	var wg sync.WaitGroup
	got := make([]*Authorizer, 16)
	for i := range got {
		wg.Go(func() {
			got[i], _ = e.Authorizer(t.Context())
		})
	}
	wg.Wait()
	for i, a := range got {
		if a == nil || a.Generation() != 3 || a != got[0] {
			t.Fatalf("request %d got %+v, want the one generation-3 snapshot", i, a)
		}
	}
	if n := src.loads.Load(); n != 1 {
		t.Errorf("loaded %d times for one change, want 1", n)
	}
	if e.Base() != got[0] {
		t.Error("Base is not the latest snapshot")
	}
}

// Two replicas on one store: a change made through one is seen by the other.
func TestEnginesShareASource(t *testing.T) {
	t.Parallel()

	src := &countingSource{}
	a, _ := NewEngine(src)
	b, _ := NewEngine(src)
	src.generation.Add(1)
	for name, e := range map[string]*Engine{"a": a, "b": b} {
		if got, err := e.Authorizer(t.Context()); err != nil || got.Generation() != 1 {
			t.Errorf("engine %s at generation %v, %v", name, got, err)
		}
	}
}

func TestEngineFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("database is locked")
	tests := []struct {
		name string
		src  *countingSource
	}{
		{"generation unreadable", &countingSource{genErr: boom}},
		{"model unreadable", &countingSource{loadErr: boom}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.src.generation.Store(1)
			e, _ := NewEngine(tt.src)
			if _, err := e.Authorizer(t.Context()); !errors.Is(err, boom) {
				t.Errorf("Authorizer() = %v, want the source's error", err)
			}
			if e.Base().Generation() != 0 {
				t.Error("a failed rebuild replaced the snapshot")
			}
		})
	}

	static, _ := NewEngine(nil)
	if a, err := static.Authorizer(t.Context()); err != nil || a != static.Base() {
		t.Errorf("an engine without a source = %v, %v", a, err)
	}
}

// The record actions are part of the coarse ones, so the roles keep what they had.
func TestActionGroups(t *testing.T) {
	t.Parallel()

	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	template := Resource{Type: "Template", ID: "t1"}
	tests := []struct {
		role   Role
		action string
		want   bool
	}{
		{RoleViewer, ActionRead, true},
		{RoleViewer, ActionUpdate, false},
		{RoleViewer, ActionCreate, false},
		{RoleEditor, ActionRead, true},
		{RoleEditor, ActionCreate, true},
		{RoleEditor, ActionUpdate, true},
		{RoleEditor, ActionDelete, true},
		{RoleEditor, ActionAttach, true},
		{RoleEditor, ActionShare, false},
		{RoleEditor, ActionTransfer, false},
		{RoleAdmin, ActionShare, true},
		{RoleAdmin, ActionTransfer, true},
		{RoleNone, ActionRead, false},
	}
	for _, tt := range tests {
		if got := a.Allow("s", []Role{tt.role}, tt.action, template); got != tt.want {
			t.Errorf("%s %s = %v, want %v", tt.role, tt.action, got, tt.want)
		}
	}
}

func TestOverlayPrefersTheRequestsEntities(t *testing.T) {
	t.Parallel()

	a, _ := New()
	admin := roleEntities()
	o := overlay{base: a.entities, extra: admin}
	for uid := range admin {
		if _, ok := o.Get(uid); !ok {
			t.Errorf("%v missing", uid)
		}
	}
	if _, ok := o.Get(actionUID(ActionRead)); !ok {
		t.Error("the base's action entities are not visible")
	}
	if _, ok := o.Get(actionUID("nothing")); ok {
		t.Error("an unknown entity was found")
	}
}
