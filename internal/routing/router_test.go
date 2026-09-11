package routing

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

type stubStore struct {
	routes []models.Route
	err    error
}

func (s stubStore) Close() error { return nil }

func (s stubStore) ListTemplates() ([]models.Template, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateTemplate(t models.Template) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) UpdateTemplate(t models.Template) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) DeleteTemplate(id string) error { return store.ErrNotFound }

func (s stubStore) GetTemplate(id string) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) ListDestinations() ([]models.Destination, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateDestination(d models.Destination) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) UpdateDestination(d models.Destination) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) DeleteDestination(id string) error { return store.ErrNotFound }

func (s stubStore) GetDestination(id string) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) ListRoutes() ([]models.Route, error) {
	return s.routes, s.err
}

func (s stubStore) CreateRoute(r models.Route) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) UpdateRoute(r models.Route) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) DeleteRoute(id string) error { return store.ErrNotFound }

func (s stubStore) GetRoute(id string) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) UpsertActiveAlert(a models.ActiveAlert) error { return store.ErrNotFound }

func (s stubStore) ListActiveAlerts(fingerprint string) ([]models.ActiveAlert, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) GetActiveAlert(fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	return models.ActiveAlert{}, store.ErrNotFound
}

func (s stubStore) DeleteActiveAlert(fingerprint, teamID, channelID string) error {
	return store.ErrNotFound
}

func (s stubStore) Ping() error { return nil }

func (s stubStore) WithTx(func(store.Store) error) error { return store.ErrNotFound }

func (s stubStore) ListGrants() ([]models.Grant, error) { return nil, store.ErrNotFound }

func (s stubStore) CreateGrant(models.Grant) (models.Grant, error) {
	return models.Grant{}, store.ErrNotFound
}

func (s stubStore) DeleteGrant(string) error { return store.ErrNotFound }

func (s stubStore) CreateSession(models.Session) error { return store.ErrNotFound }

func (s stubStore) GetSession(string) (models.Session, error) {
	return models.Session{}, store.ErrNotFound
}

func (s stubStore) DeleteSession(string) error { return store.ErrNotFound }

func (s stubStore) DeleteExpiredSessions() error { return store.ErrNotFound }

func (s stubStore) CreateLoginFlow(models.LoginFlow) error { return store.ErrNotFound }

func (s stubStore) TakeLoginFlow(string) (models.LoginFlow, error) {
	return models.LoginFlow{}, store.ErrNotFound
}

func planOf(t *testing.T, routes []models.Route, labels map[string]string) Result {
	t.Helper()

	result, err := New(stubStore{routes: routes}).Plan(labels)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return result
}

func deliveredBy(result Result) []string {
	out := make([]string, 0, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		out = append(out, delivery.RouteID)
	}
	return out
}

// Root selection is unchanged by nesting: highest priority first, ties by name,
// the default last.
func TestPlanPicksTheRoot(t *testing.T) {
	t.Parallel()

	routes := []models.Route{
		{ID: "default", Name: "default", IsDefault: true, DestinationID: "d1", TemplateID: "t1", Priority: 1},
		{ID: "critical", Name: "critical", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "d2", TemplateID: "t2", Priority: 50},
		{ID: "critical-gold", Name: "critical-gold", LabelSelector: map[string]string{"severity": "critical", "tier": "gold"}, DestinationID: "d3", TemplateID: "t3", Priority: 100},
		{ID: "empty", Name: "empty", LabelSelector: map[string]string{}, Priority: 200},
	}

	tests := []struct {
		name       string
		routes     []models.Route
		labels     map[string]string
		wantReason Reason
		wantRoutes []string
	}{
		{
			name: "the highest priority selector wins", routes: routes,
			labels:     map[string]string{"severity": "critical", "tier": "gold"},
			wantReason: ReasonSelector, wantRoutes: []string{"critical-gold"},
		},
		{
			name: "every selector label must match", routes: routes,
			labels:     map[string]string{"severity": "critical"},
			wantReason: ReasonSelector, wantRoutes: []string{"critical"},
		},
		{
			name: "an empty selector never matches, even at the highest priority", routes: routes,
			labels:     map[string]string{"anything": "at-all"},
			wantReason: ReasonDefault, wantRoutes: []string{"default"},
		},
		{
			name: "nothing matches and there is no default", routes: routes[1:2],
			labels:     map[string]string{"severity": "warning"},
			wantReason: ReasonNone,
		},
		{
			name:       "no routes at all",
			labels:     map[string]string{"severity": "critical"},
			wantReason: ReasonNoRoutes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := planOf(t, tt.routes, tt.labels)
			if result.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", result.Reason, tt.wantReason)
			}
			if got := deliveredBy(result); !equalStrings(got, tt.wantRoutes) {
				t.Errorf("deliveries = %v, want %v", got, tt.wantRoutes)
			}
		})
	}
}

// The point of the tree: a child sends the alert somewhere else, either as well
// as its parent or instead of it.
func TestPlanNesting(t *testing.T) {
	t.Parallel()

	parent := models.Route{ID: "parent", Name: "parent", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "ops", TemplateID: "card", Priority: 100}

	tests := []struct {
		name       string
		routes     []models.Route
		labels     map[string]string
		wantRoutes []string
	}{
		{
			name: "a non-greedy child delivers as well as its parent",
			routes: []models.Route{parent,
				{ID: "child", Name: "child", ParentID: "parent", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
			},
			labels:     map[string]string{"severity": "critical", "team": "payments"},
			wantRoutes: []string{"parent", "child"},
		},
		{
			name: "a greedy child delivers instead of its parent",
			routes: []models.Route{parent,
				{ID: "child", Name: "child", ParentID: "parent", Greedy: true, LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
			},
			labels:     map[string]string{"severity": "critical", "team": "payments"},
			wantRoutes: []string{"child"},
		},
		{
			name: "a child whose selector does not match leaves the parent alone",
			routes: []models.Route{parent,
				{ID: "child", Name: "child", ParentID: "parent", Greedy: true, LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
			},
			labels:     map[string]string{"severity": "critical", "team": "search"},
			wantRoutes: []string{"parent"},
		},
		{
			name: "every matching child delivers",
			routes: []models.Route{parent,
				{ID: "a", Name: "a", ParentID: "parent", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments", Priority: 10},
				{ID: "b", Name: "b", ParentID: "parent", LabelSelector: map[string]string{"tier": "gold"}, DestinationID: "gold", Priority: 5},
			},
			labels:     map[string]string{"severity": "critical", "team": "payments", "tier": "gold"},
			wantRoutes: []string{"parent", "a", "b"},
		},
		{
			// One greedy child is enough to take the delivery off the parent; the
			// other children still deliver.
			name: "one greedy child among several suppresses the parent",
			routes: []models.Route{parent,
				{ID: "a", Name: "a", ParentID: "parent", Greedy: true, LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments", Priority: 10},
				{ID: "b", Name: "b", ParentID: "parent", LabelSelector: map[string]string{"tier": "gold"}, DestinationID: "gold", Priority: 5},
			},
			labels:     map[string]string{"severity": "critical", "team": "payments", "tier": "gold"},
			wantRoutes: []string{"a", "b"},
		},
		{
			name: "a grandchild refines a child",
			routes: []models.Route{parent,
				{ID: "child", Name: "child", ParentID: "parent", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
				{ID: "grandchild", Name: "grandchild", ParentID: "child", LabelSelector: map[string]string{"tier": "gold"}, DestinationID: "gold"},
			},
			labels:     map[string]string{"severity": "critical", "team": "payments", "tier": "gold"},
			wantRoutes: []string{"parent", "child", "grandchild"},
		},
		{
			// Its parent never matched, so the child is never reached.
			name: "a child of a parent that did not match stays out of it",
			routes: []models.Route{parent,
				{ID: "other", Name: "other", IsDefault: true, DestinationID: "ops", TemplateID: "card"},
				{ID: "child", Name: "child", ParentID: "parent", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
			},
			labels:     map[string]string{"team": "payments"},
			wantRoutes: []string{"other"},
		},
		{
			// Dropping it would make the route unreachable without saying so.
			name: "a child whose parent was deleted is treated as a root",
			routes: []models.Route{
				{ID: "orphan", Name: "orphan", ParentID: "gone", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "ops", TemplateID: "card"},
			},
			labels:     map[string]string{"severity": "critical"},
			wantRoutes: []string{"orphan"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := deliveredBy(planOf(t, tt.routes, tt.labels)); !equalStrings(got, tt.wantRoutes) {
				t.Errorf("deliveries = %v, want %v", got, tt.wantRoutes)
			}
		})
	}
}

// A child that sets only a destination still renders with its parent's template,
// which is what makes "same card, another channel" a one-field route.
func TestPlanInheritsDestinationAndTemplate(t *testing.T) {
	t.Parallel()

	routes := []models.Route{
		{ID: "parent", Name: "parent", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "ops", TemplateID: "card", Priority: 100},
		{ID: "channel-only", Name: "channel-only", ParentID: "parent", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
		{ID: "template-only", Name: "template-only", ParentID: "channel-only", LabelSelector: map[string]string{"tier": "gold"}, TemplateID: "gold-card"},
	}

	result := planOf(t, routes, map[string]string{"severity": "critical", "team": "payments", "tier": "gold"})

	want := map[string][2]string{
		"parent":        {"ops", "card"},
		"channel-only":  {"payments", "card"},
		"template-only": {"payments", "gold-card"},
	}
	if len(result.Deliveries) != len(want) {
		t.Fatalf("deliveries = %v, want %d of them", deliveredBy(result), len(want))
	}
	for _, delivery := range result.Deliveries {
		if got := [2]string{delivery.DestinationID, delivery.TemplateID}; got != want[delivery.RouteID] {
			t.Errorf("%s delivers to %v, want %v", delivery.RouteID, got, want[delivery.RouteID])
		}
	}
}

// A child delivery says it refined a parent rather than that it matched on its
// own, because that is what the explanation has to say.
func TestPlanReportsWhyEachDeliveryHappened(t *testing.T) {
	t.Parallel()

	result := planOf(t, []models.Route{
		{ID: "parent", Name: "parent", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "ops", TemplateID: "card"},
		{ID: "child", Name: "child", ParentID: "parent", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
	}, map[string]string{"severity": "critical", "team": "payments"})

	if result.RootID != "parent" || result.RootName != "parent" {
		t.Errorf("root = %q/%q, want the route that matched first", result.RootID, result.RootName)
	}
	if result.Deliveries[0].Reason != ReasonSelector {
		t.Errorf("parent reason = %q, want %q", result.Deliveries[0].Reason, ReasonSelector)
	}
	if result.Deliveries[1].Reason != ReasonRefined {
		t.Errorf("child reason = %q, want %q", result.Deliveries[1].Reason, ReasonRefined)
	}
}

// A tree broken into a cycle must not take delivery with it.
func TestPlanStopsAtMaxDepth(t *testing.T) {
	t.Parallel()

	routes := []models.Route{
		{ID: "a", Name: "a", ParentID: "b", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "one", TemplateID: "card"},
		{ID: "b", Name: "b", ParentID: "a", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "two", TemplateID: "card"},
	}

	done := make(chan Result, 1)
	go func() {
		result, err := New(stubStore{routes: routes}).Plan(map[string]string{"severity": "critical"})
		if err != nil {
			t.Errorf("Plan: %v", err)
		}
		done <- result
	}()

	select {
	case result := <-done:
		if len(result.Deliveries) > MaxDepth+1 {
			t.Errorf("deliveries = %v, want the walk to stop at MaxDepth", deliveredBy(result))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Plan did not return, so a cycle in the tree hangs delivery")
	}
}

func TestPlanReportsStoreFailures(t *testing.T) {
	t.Parallel()

	if _, err := New(stubStore{err: errors.New("store down")}).Plan(map[string]string{"severity": "critical"}); err == nil {
		t.Fatal("Plan() = nil error, want the store failure to surface")
	}
}

func TestValidateRoute(t *testing.T) {
	t.Parallel()

	existing := []models.Route{
		{ID: "root", Name: "root", DestinationID: "ops", TemplateID: "card"},
		{ID: "child", Name: "child", ParentID: "root", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "payments"},
		{ID: "grandchild", Name: "grandchild", ParentID: "child", LabelSelector: map[string]string{"tier": "gold"}, DestinationID: "gold"},
	}

	tests := []struct {
		name      string
		candidate models.Route
		wantErr   string
	}{
		{
			name:      "a root route is unconstrained",
			candidate: models.Route{ID: "new", Name: "new"},
		},
		{
			name:      "a child refining its parent is fine",
			candidate: models.Route{ID: "new", Name: "new", ParentID: "root", LabelSelector: map[string]string{"team": "search"}, DestinationID: "search"},
		},
		{
			name:      "the parent has to exist",
			candidate: models.Route{ID: "new", Name: "new", ParentID: "gone", LabelSelector: map[string]string{"team": "search"}, DestinationID: "search"},
			wantErr:   "does not exist",
		},
		{
			name:      "a route cannot be its own parent",
			candidate: models.Route{ID: "root", Name: "root", ParentID: "root", LabelSelector: map[string]string{"team": "search"}, DestinationID: "search"},
			wantErr:   "its own parent",
		},
		{
			name:      "a cycle is refused",
			candidate: models.Route{ID: "root", Name: "root", ParentID: "grandchild", LabelSelector: map[string]string{"team": "search"}, DestinationID: "search"},
			wantErr:   "cycle",
		},
		{
			name:      "a child without a selector could never fire",
			candidate: models.Route{ID: "new", Name: "new", ParentID: "root", DestinationID: "search"},
			wantErr:   "needs a label selector",
		},
		{
			name:      "a child that inherits everything changes nothing",
			candidate: models.Route{ID: "new", Name: "new", ParentID: "root", LabelSelector: map[string]string{"team": "search"}},
			wantErr:   "changes nothing",
		},
		{
			name:      "only a root can be the default",
			candidate: models.Route{ID: "new", Name: "new", ParentID: "root", IsDefault: true, LabelSelector: map[string]string{"team": "search"}, DestinationID: "search"},
			wantErr:   "only a root route",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRoute(tt.candidate, existing)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("ValidateRoute() = %v, want it accepted", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("ValidateRoute() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

// Deleting a parent would promote its children to roots, where their selectors
// match alerts their parent used to filter out.
func TestValidateDelete(t *testing.T) {
	t.Parallel()

	existing := []models.Route{
		{ID: "root", Name: "root"},
		{ID: "child", Name: "child", ParentID: "root"},
	}

	if err := ValidateDelete("root", existing); err == nil {
		t.Error("ValidateDelete() = nil, want a refusal while the route has children")
	}
	if err := ValidateDelete("child", existing); err != nil {
		t.Errorf("ValidateDelete() = %v, want a leaf to be deletable", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
