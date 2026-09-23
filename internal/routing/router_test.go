package routing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

type stubStore struct {
	routes []models.Route
	err    error
	// fallback is the global default destination; empty means there is none.
	fallback   string
	defaultErr error
}

func (s stubStore) Close() error { return nil }

func (s stubStore) ListTemplates(ctx context.Context) ([]models.Template, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) DeleteTemplate(ctx context.Context, id string) error { return store.ErrNotFound }

func (s stubStore) GetTemplate(ctx context.Context, id string) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) ListDestinations(ctx context.Context) ([]models.Destination, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) DeleteDestination(ctx context.Context, id string) error { return store.ErrNotFound }

func (s stubStore) GetDestination(ctx context.Context, id string) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) GetDefaultDestination(ctx context.Context) (models.Destination, error) {
	if s.defaultErr != nil {
		return models.Destination{}, s.defaultErr
	}
	if s.fallback == "" {
		return models.Destination{}, store.ErrNotFound
	}
	return models.Destination{ID: s.fallback, IsDefault: true}, nil
}

func (s stubStore) SetDefaultDestination(ctx context.Context, id string) error {
	return store.ErrNotFound
}

func (s stubStore) ListRecipients(ctx context.Context) ([]models.Recipient, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	return models.Recipient{}, store.ErrNotFound
}

func (s stubStore) UpdateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	return models.Recipient{}, store.ErrNotFound
}

func (s stubStore) DeleteRecipient(ctx context.Context, id string) error { return store.ErrNotFound }

func (s stubStore) GetRecipient(ctx context.Context, id string) (models.Recipient, error) {
	return models.Recipient{}, store.ErrNotFound
}

func (s stubStore) GetRecipientByConversation(ctx context.Context, conversationID string) (models.Recipient, error) {
	return models.Recipient{}, store.ErrNotFound
}

func (s stubStore) GetRecipientBySubject(ctx context.Context, subject string) (models.Recipient, error) {
	return models.Recipient{}, store.ErrNotFound
}

func (s stubStore) ListWebhookEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	return models.WebhookEndpoint{}, store.ErrNotFound
}

func (s stubStore) UpdateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	return models.WebhookEndpoint{}, store.ErrNotFound
}

func (s stubStore) RotateWebhookEndpointToken(ctx context.Context, id, tokenHash string) error {
	return store.ErrNotFound
}

func (s stubStore) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	return store.ErrNotFound
}

func (s stubStore) GetWebhookEndpoint(ctx context.Context, id string) (models.WebhookEndpoint, error) {
	return models.WebhookEndpoint{}, store.ErrNotFound
}

func (s stubStore) GetWebhookEndpointBySlug(ctx context.Context, teamSlug, channelSlug string) (models.WebhookEndpoint, error) {
	return models.WebhookEndpoint{}, store.ErrNotFound
}

func (s stubStore) MarkRecipientBlocked(ctx context.Context, id string, at time.Time, reason string) error {
	return store.ErrNotFound
}

func (s stubStore) ClearRecipientBlocked(ctx context.Context, id string) error {
	return store.ErrNotFound
}

func (s stubStore) CreateLinkFlow(ctx context.Context, f models.LinkFlow) error {
	return store.ErrNotFound
}

func (s stubStore) TakeLinkFlow(ctx context.Context, code string) (models.LinkFlow, error) {
	return models.LinkFlow{}, store.ErrNotFound
}

func (s stubStore) DeleteLinkFlowsForSubject(ctx context.Context, subject string) error {
	return nil
}

func (s stubStore) ListRoutes(ctx context.Context) ([]models.Route, error) {
	return s.routes, s.err
}

func (s stubStore) CreateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) UpdateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) DeleteRoute(ctx context.Context, id string) error { return store.ErrNotFound }

func (s stubStore) GetRoute(ctx context.Context, id string) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) ClaimActiveAlert(ctx context.Context, _ models.AlertClaim) (models.ActiveAlert, store.ClaimOutcome, error) {
	return models.ActiveAlert{}, store.ClaimHeld, store.ErrNotFound
}

func (s stubStore) CompleteActiveAlertClaim(ctx context.Context, _ models.AlertClaim, _ string, _ time.Time) error {
	return store.ErrNotFound
}

func (s stubStore) ReleaseActiveAlertClaim(ctx context.Context, _ models.AlertClaim) error {
	return store.ErrNotFound
}

func (s stubStore) TouchActiveAlert(ctx context.Context, _ models.ActiveAlert, _ string, _ time.Time) error {
	return store.ErrNotFound
}

func (s stubStore) ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CountActiveAlerts(ctx context.Context) (int64, error) { return 0, store.ErrNotFound }

func (s stubStore) GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	return models.ActiveAlert{}, store.ErrNotFound
}

func (s stubStore) DeleteActiveAlertCard(ctx context.Context, _, _, _, _ string) error {
	return store.ErrNotFound
}

// The chat half of the claim protocol. Routing never reaches it -- planning is
// what this package does, delivery is the server's -- but Store is one
// interface, so it is implemented here for the compiler.
func (s stubStore) ClaimActiveAlertRecipient(ctx context.Context, _ models.RecipientClaim) (models.ActiveAlertRecipient, store.ClaimOutcome, error) {
	return models.ActiveAlertRecipient{}, store.ClaimHeld, store.ErrNotFound
}

func (s stubStore) CompleteActiveAlertRecipientClaim(ctx context.Context, _ models.RecipientClaim, _ string, _ time.Time) error {
	return store.ErrNotFound
}

func (s stubStore) ReleaseActiveAlertRecipientClaim(ctx context.Context, _ models.RecipientClaim) error {
	return store.ErrNotFound
}

func (s stubStore) TouchActiveAlertRecipient(ctx context.Context, _ models.ActiveAlertRecipient, _ string, _ time.Time) error {
	return store.ErrNotFound
}

func (s stubStore) ListActiveAlertRecipients(ctx context.Context, fingerprint string) ([]models.ActiveAlertRecipient, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) DeleteActiveAlertRecipientCard(ctx context.Context, _, _, _ string) error {
	return store.ErrNotFound
}

func (s stubStore) DeleteActiveAlertRecipientsFor(ctx context.Context, recipientID string) error {
	return store.ErrNotFound
}

func (s stubStore) Ping(ctx context.Context) error { return nil }

func (s stubStore) WithSerializableTx(ctx context.Context, _ func(context.Context, store.Store) error) error {
	return store.ErrNotFound
}

func (s stubStore) WithTx(ctx context.Context, _ func(context.Context, store.Store) error) error {
	return store.ErrNotFound
}

func (s stubStore) ListGrants(ctx context.Context) ([]models.Grant, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateGrant(ctx context.Context, _ models.Grant) (models.Grant, error) {
	return models.Grant{}, store.ErrNotFound
}

func (s stubStore) DeleteGrant(ctx context.Context, _ string) error { return store.ErrNotFound }

func (s stubStore) DeleteGrantsForRole(ctx context.Context, _ string) error { return store.ErrNotFound }

func (s stubStore) CreateSession(ctx context.Context, _ models.Session) error {
	return store.ErrNotFound
}

func (s stubStore) GetSession(ctx context.Context, _ string) (models.Session, error) {
	return models.Session{}, store.ErrNotFound
}

func (s stubStore) DeleteSession(ctx context.Context, _ string) error { return store.ErrNotFound }

func (s stubStore) DeleteExpiredSessions(ctx context.Context) error { return store.ErrNotFound }

func (s stubStore) CreateBrokerToken(ctx context.Context, _ models.BrokerToken) error {
	return store.ErrNotFound
}

func (s stubStore) GetBrokerToken(ctx context.Context, _ string) (models.BrokerToken, error) {
	return models.BrokerToken{}, store.ErrNotFound
}

func (s stubStore) UpdateBrokerToken(ctx context.Context, _ models.BrokerToken) error {
	return store.ErrNotFound
}

func (s stubStore) DeleteBrokerToken(ctx context.Context, _ string) error { return store.ErrNotFound }

func (s stubStore) RecordAlertSamples(ctx context.Context, _ []models.AlertSample) error {
	return store.ErrNotFound
}

func (s stubStore) ListAlertSamples(ctx context.Context, _ int) ([]models.AlertSample, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) PruneAlertSamples(ctx context.Context, _ time.Time, _ int) (int64, error) {
	return 0, store.ErrNotFound
}

func (s stubStore) CreateLoginFlow(ctx context.Context, _ models.LoginFlow) error {
	return store.ErrNotFound
}

func (s stubStore) TakeLoginFlow(ctx context.Context, _ string) (models.LoginFlow, error) {
	return models.LoginFlow{}, store.ErrNotFound
}

func planOf(t *testing.T, routes []models.Route, labels map[string]string) Result {
	t.Helper()

	result, err := New(stubStore{routes: routes}).Plan(t.Context(), labels)
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

// Every non-default root whose selector matches delivers, in priority order,
// ties by name; the default is a fallback and fires only when nothing else
// matched at all.
func TestPlanPicksEveryMatchingRoot(t *testing.T) {
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
			// "critical" selects on severity alone, so it matches too -- an
			// alert carrying an extra label is still every bit as critical.
			name: "two selectors both matching both deliver, highest priority first", routes: routes,
			labels:     map[string]string{"severity": "critical", "tier": "gold"},
			wantReason: ReasonSelector, wantRoutes: []string{"critical-gold", "critical"},
		},
		{
			name: "one selector matches", routes: routes,
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

// The headline behavior: two independent root routes, sharing nothing but one
// label value, both fire -- nothing suppresses one in favour of the other the
// way a single winning root used to.
func TestPlanFansOutAcrossIndependentRoots(t *testing.T) {
	t.Parallel()

	routes := []models.Route{
		{ID: "ops", Name: "ops", LabelSelector: map[string]string{"team": "ops"}, DestinationID: "d-ops", TemplateID: "t1", Priority: 100},
		{ID: "audit", Name: "audit", LabelSelector: map[string]string{"team": "ops"}, DestinationID: "d-audit", TemplateID: "t2", Priority: 50},
		{ID: "elsewhere", Name: "elsewhere", LabelSelector: map[string]string{"team": "payments"}, DestinationID: "d-elsewhere", TemplateID: "t3"},
	}

	result := planOf(t, routes, map[string]string{"team": "ops"})

	if result.Reason != ReasonSelector {
		t.Fatalf("reason = %q, want %q", result.Reason, ReasonSelector)
	}
	if !equalStrings(result.Roots, []string{"ops", "audit"}) {
		t.Errorf("roots = %v, want both matched roots, priority order", result.Roots)
	}
	if got := deliveredBy(result); !equalStrings(got, []string{"ops", "audit"}) {
		t.Errorf("deliveries = %v, want one from each matched root", got)
	}
	for i, wantDest := range []string{"d-ops", "d-audit"} {
		if result.Deliveries[i].DestinationID != wantDest {
			t.Errorf("delivery %d destination = %q, want %q", i, result.Deliveries[i].DestinationID, wantDest)
		}
		if result.Deliveries[i].Reason != ReasonSelector {
			t.Errorf("delivery %d reason = %q, want %q", i, result.Deliveries[i].Reason, ReasonSelector)
		}
	}
}

// The default never adds itself alongside a real match -- it is what an alert
// falls back to, not one more route with a low priority.
func TestPlanDefaultDoesNotJoinARealMatch(t *testing.T) {
	t.Parallel()

	routes := []models.Route{
		{ID: "default", Name: "default", IsDefault: true, DestinationID: "d-default", TemplateID: "t1"},
		{ID: "critical", Name: "critical", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "d-critical", TemplateID: "t2"},
	}

	result := planOf(t, routes, map[string]string{"severity": "critical"})

	if !equalStrings(result.Roots, []string{"critical"}) {
		t.Errorf("roots = %v, want only the matched selector, not the default too", result.Roots)
	}
	if got := deliveredBy(result); !equalStrings(got, []string{"critical"}) {
		t.Errorf("deliveries = %v, want only the matched selector's", got)
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

	if !equalStrings(result.Roots, []string{"parent"}) {
		t.Errorf("roots = %v, want the route that matched", result.Roots)
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
		result, err := New(stubStore{routes: routes}).Plan(t.Context(), map[string]string{"severity": "critical"})
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

	if _, err := New(stubStore{err: errors.New("store down")}).Plan(t.Context(), map[string]string{"severity": "critical"}); err == nil {
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

// deliveredTo describes a plan as "which route, to what" so a fan-out across
// two transports reads as one list.
func deliveredTo(result Result) []string {
	out := make([]string, 0, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		target := delivery.DestinationID
		if delivery.Kind == DeliveryRecipient {
			target = delivery.RecipientID
		}
		out = append(out, fmt.Sprintf("%s→%s:%s", delivery.RouteID, delivery.Kind, target))
	}
	return out
}

// A route now has two independent targets, so one route can produce two
// deliveries -- and the order between them has to be stable, because callers
// index into the slice.
func TestPlanFansOutToBothTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		routes []models.Route
		labels map[string]string
		want   []string
	}{
		{
			name: "a route naming both delivers twice, channel first",
			routes: []models.Route{
				{ID: "r", Name: "r", LabelSelector: map[string]string{"a": "b"}, DestinationID: "d1", RecipientID: "p1", TemplateID: "t1"},
			},
			labels: map[string]string{"a": "b"},
			want:   []string{"r→channel:d1", "r→recipient:p1"},
		},
		{
			name: "a route naming only a person delivers once",
			routes: []models.Route{
				{ID: "r", Name: "r", LabelSelector: map[string]string{"a": "b"}, RecipientID: "p1", TemplateID: "t1"},
			},
			labels: map[string]string{"a": "b"},
			want:   []string{"r→recipient:p1"},
		},
		{
			// Neither target set anywhere up the tree: there is nothing to
			// deliver to, so nothing is delivered rather than a Delivery
			// pointing at "".
			name: "a route naming no target at all delivers nothing",
			routes: []models.Route{
				{ID: "r", Name: "r", LabelSelector: map[string]string{"a": "b"}, TemplateID: "t1"},
			},
			labels: map[string]string{"a": "b"},
			want:   []string{},
		},
		{
			name: "a child inherits both targets from its parent",
			routes: []models.Route{
				{ID: "p", Name: "p", LabelSelector: map[string]string{"a": "b"}, DestinationID: "d1", RecipientID: "p1", TemplateID: "t1"},
				{ID: "c", Name: "c", ParentID: "p", LabelSelector: map[string]string{"x": "y"}, TemplateID: "t2"},
			},
			labels: map[string]string{"a": "b", "x": "y"},
			want: []string{
				"p→channel:d1", "p→recipient:p1",
				"c→channel:d1", "c→recipient:p1",
			},
		},
		{
			// Overriding one target must not disturb the other.
			name: "a child overriding only the recipient keeps its parent's channel",
			routes: []models.Route{
				{ID: "p", Name: "p", LabelSelector: map[string]string{"a": "b"}, DestinationID: "d1", RecipientID: "p1", TemplateID: "t1"},
				{ID: "c", Name: "c", ParentID: "p", LabelSelector: map[string]string{"x": "y"}, RecipientID: "p2"},
			},
			labels: map[string]string{"a": "b", "x": "y"},
			want: []string{
				"p→channel:d1", "p→recipient:p1",
				"c→channel:d1", "c→recipient:p2",
			},
		},
		{
			// The one ADR 0026 is explicit about: greedy is one flag per
			// parent frame, so it takes both of the parent's deliveries or
			// neither. Suppressing only the matching kind would leave the
			// parent still delivering to the other.
			name: "a greedy child suppresses both of its parent's deliveries",
			routes: []models.Route{
				{ID: "p", Name: "p", LabelSelector: map[string]string{"a": "b"}, DestinationID: "d1", RecipientID: "p1", TemplateID: "t1"},
				{ID: "c", Name: "c", ParentID: "p", Greedy: true, LabelSelector: map[string]string{"x": "y"}, DestinationID: "d2"},
			},
			labels: map[string]string{"a": "b", "x": "y"},
			want:   []string{"c→channel:d2", "c→recipient:p1"},
		},
		{
			// A greedy child that names only a person still takes the parent's
			// channel delivery with it.
			name: "a greedy child naming only a person still suppresses the channel",
			routes: []models.Route{
				{ID: "p", Name: "p", LabelSelector: map[string]string{"a": "b"}, DestinationID: "d1", TemplateID: "t1"},
				{ID: "c", Name: "c", ParentID: "p", Greedy: true, LabelSelector: map[string]string{"x": "y"}, RecipientID: "p1"},
			},
			labels: map[string]string{"a": "b", "x": "y"},
			want:   []string{"c→channel:d1", "c→recipient:p1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := deliveredTo(planOf(t, tt.routes, tt.labels))
			if !slices.Equal(got, tt.want) {
				t.Errorf("deliveries = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every delivery says which transport it goes out through, because the two are
// told apart nowhere else: a chat delivery carries no destination and a channel
// delivery carries no recipient.
func TestDeliveryKindIsAlwaysSet(t *testing.T) {
	t.Parallel()

	result := planOf(t, []models.Route{
		{ID: "r", Name: "r", LabelSelector: map[string]string{"a": "b"}, DestinationID: "d1", RecipientID: "p1", TemplateID: "t1"},
	}, map[string]string{"a": "b"})

	if len(result.Deliveries) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(result.Deliveries))
	}
	channel, recipient := result.Deliveries[0], result.Deliveries[1]
	if channel.Kind != DeliveryChannel || channel.RecipientID != "" {
		t.Errorf("channel delivery = %+v, want kind channel and no recipient", channel)
	}
	if recipient.Kind != DeliveryRecipient || recipient.DestinationID != "" {
		t.Errorf("recipient delivery = %+v, want kind recipient and no destination", recipient)
	}
}

// A child that sets only a recipient does change something, so the clause that
// rejects a child changing nothing has to be an AND over all three targets.
func TestValidateRouteAcceptsARecipientOnlyChild(t *testing.T) {
	t.Parallel()

	existing := []models.Route{{ID: "p", Name: "p", DestinationID: "d1", TemplateID: "t1"}}

	child := models.Route{
		ID: "c", Name: "c", ParentID: "p",
		LabelSelector: map[string]string{"x": "y"},
		RecipientID:   "p1",
	}
	if err := ValidateRoute(child, existing); err != nil {
		t.Errorf("a child setting only a recipient = %v, want accepted", err)
	}

	// And one that still sets nothing is still rejected.
	nothing := models.Route{ID: "c2", Name: "c2", ParentID: "p", LabelSelector: map[string]string{"x": "y"}}
	if err := ValidateRoute(nothing, existing); err == nil {
		t.Error("a child inheriting all three targets = nil, want rejected")
	}
}

// The global default destination catches only what no route, default route
// included, claimed.
func TestPlanFallsBackToTheGlobalDefault(t *testing.T) {
	t.Parallel()

	critical := models.Route{ID: "critical", Name: "critical", LabelSelector: map[string]string{"severity": "critical"}, DestinationID: "d-critical"}
	defaultRoute := models.Route{ID: "default", Name: "default", IsDefault: true, DestinationID: "d-default"}

	cases := []struct {
		name     string
		routes   []models.Route
		fallback string
		labels   map[string]string
		reason   Reason
		want     string
	}{
		{name: "no routes", fallback: "d-global", reason: ReasonGlobalDefault, want: "d-global"},
		{name: "no match, no default route", routes: []models.Route{critical}, fallback: "d-global",
			labels: map[string]string{"severity": "info"}, reason: ReasonGlobalDefault, want: "d-global"},
		{name: "default route wins", routes: []models.Route{critical, defaultRoute}, fallback: "d-global",
			labels: map[string]string{"severity": "info"}, reason: ReasonDefault, want: "d-default"},
		{name: "selector wins", routes: []models.Route{critical}, fallback: "d-global",
			labels: map[string]string{"severity": "critical"}, reason: ReasonSelector, want: "d-critical"},
		{name: "no routes, no destination", reason: ReasonNoRoutes},
		{name: "no match, no destination", routes: []models.Route{critical},
			labels: map[string]string{"severity": "info"}, reason: ReasonNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := New(stubStore{routes: tc.routes, fallback: tc.fallback}).Plan(t.Context(), tc.labels)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if result.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", result.Reason, tc.reason)
			}
			if tc.want == "" {
				if len(result.Deliveries) != 0 {
					t.Errorf("deliveries = %+v, want none", result.Deliveries)
				}
				return
			}
			if len(result.Deliveries) != 1 || result.Deliveries[0].DestinationID != tc.want {
				t.Fatalf("deliveries = %+v, want one to %q", result.Deliveries, tc.want)
			}
			if tc.reason == ReasonGlobalDefault {
				got := result.Deliveries[0]
				if got.RouteID != GlobalDefaultRouteID || got.TemplateID != "" || got.Kind != DeliveryChannel {
					t.Errorf("delivery = %+v, want the synthetic route with no template", got)
				}
			}
		})
	}
}

func TestPlanReportsAGlobalDefaultReadFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	if _, err := New(stubStore{defaultErr: boom}).Plan(t.Context(), nil); !errors.Is(err, boom) {
		t.Errorf("Plan = %v, want %v", err, boom)
	}
}
