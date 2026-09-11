package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func routingStore() *fakeStore {
	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Name: "Critical card"}
	st.destinations["dest"] = models.Destination{ID: "dest", Name: "Ops channel", TeamID: "team", ChannelID: "chan"}
	st.routes["critical"] = models.Route{
		ID: "critical", Name: "Critical to ops", TemplateID: "tmpl", DestinationID: "dest",
		LabelSelector: map[string]string{"severity": "critical"}, Priority: 100,
	}
	st.routes["fallback"] = models.Route{
		ID: "fallback", Name: "Fallback", TemplateID: "tmpl", DestinationID: "dest", IsDefault: true, Priority: 1,
	}
	return st
}

func graphFrom(t *testing.T, handler http.Handler) (map[string]graphNode, []graphLink) {
	t.Helper()

	rec := do(t, handler, http.MethodGet, "/api/routing/graph", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET graph = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Nodes []graphNode `json:"nodes"`
		Links []graphLink `json:"links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode graph: %v", err)
	}

	byID := map[string]graphNode{}
	for _, node := range payload.Nodes {
		byID[node.ID] = node
	}
	return byID, payload.Links
}

func TestRoutingGraph(t *testing.T) {
	t.Parallel()

	directory := &fakeMessenger{
		teams:    []graph.Team{{ID: "team", Name: "Platform"}},
		channels: map[string][]graph.Channel{"team": {{ID: "chan", Name: "Alerts"}}},
	}
	nodes, links := graphFrom(t, newTestServer(t, routingStore(), directory).Handler)

	if got := nodes["route:critical"]; got.Kind != "route" || got.Label != "Critical to ops" || got.Selector != "severity=critical" || got.Priority != 100 {
		t.Errorf("route node = %+v, want the route resolved with its selector", got)
	}
	if got := nodes["destination:dest"]; got.Kind != "destination" || got.Label != "Ops channel" || got.Detail != "Platform › Alerts" {
		t.Errorf("destination node = %+v, want the destination name and the Team/channel names", got)
	}
	if got := nodes["template:tmpl"]; got.Label != "Critical card" {
		t.Errorf("template node = %+v, want the template name", got)
	}
	if !nodes["route:fallback"].Default {
		t.Error("the default route is not marked as one")
	}

	// The webhook source, plus two routes each pointing at a destination and a
	// template.
	if len(links) != 6 {
		t.Errorf("links = %d, want 6", len(links))
	}
	for _, link := range links {
		if _, ok := nodes[link.Target]; !ok {
			t.Errorf("link %+v points at a node that is not in the graph", link)
		}
	}
}

// A route pointing at something deleted is the state the view exists to show,
// so it must not quietly lose the link.
func TestRoutingGraphMarksDanglingReferences(t *testing.T) {
	t.Parallel()

	st := routingStore()
	delete(st.destinations, "dest")
	nodes, links := graphFrom(t, newTestServer(t, st, &fakeMessenger{}).Handler)

	missing, ok := nodes["destination:dest"]
	if !ok {
		t.Fatal("the deleted destination left no node, so the broken route looks fine")
	}
	if !missing.Missing || !strings.Contains(missing.Label, "missing") {
		t.Errorf("node = %+v, want it marked missing", missing)
	}

	found := false
	for _, link := range links {
		if link.Source == "route:critical" && link.Target == "destination:dest" {
			found = true
		}
	}
	if !found {
		t.Error("the link to the deleted destination was dropped")
	}
}

func TestRoutingGraphWithNothingConfigured(t *testing.T) {
	t.Parallel()

	nodes, links := graphFrom(t, newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler)
	if len(nodes) != 1 || nodes["source:webhook"].Kind != "source" {
		t.Errorf("graph = %v, want the webhook source alone", nodes)
	}
	if len(links) != 0 {
		t.Errorf("links = %v, want none rather than null", links)
	}
}

// The picture is the flow an alert takes, so it starts where alerts arrive and
// every route hangs off that one node.
func TestRoutingGraphFlowsFromTheWebhook(t *testing.T) {
	t.Parallel()

	nodes, links := graphFrom(t, newTestServer(t, routingStore(), &fakeMessenger{}).Handler)

	source, ok := nodes["source:webhook"]
	if !ok || source.Kind != "source" {
		t.Fatalf("nodes = %v, want a webhook source node", nodes)
	}
	if !strings.Contains(source.Detail, "/webhook/alertmanager") {
		t.Errorf("source detail = %q, want the endpoints alerts arrive on", source.Detail)
	}

	fed := map[string]bool{}
	for _, link := range links {
		if link.Source == source.ID {
			fed[link.Target] = true
		}
	}
	for _, route := range []string{"route:critical", "route:fallback"} {
		if !fed[route] {
			t.Errorf("%s is not fed by the webhook, so the flow has no start", route)
		}
	}
}

// Columns left to right: what arrives, what decides, where it lands. The rows
// follow the order the router evaluates routes in.
func TestRoutingGraphLaysOutColumns(t *testing.T) {
	t.Parallel()

	nodes, _ := graphFrom(t, newTestServer(t, routingStore(), &fakeMessenger{}).Handler)

	if x := nodes["source:webhook"].X; x != 0 {
		t.Errorf("source x = %d, want the leftmost column", x)
	}
	if nodes["route:critical"].X <= nodes["source:webhook"].X {
		t.Error("routes are not to the right of the webhook")
	}
	if nodes["destination:dest"].X <= nodes["route:critical"].X {
		t.Error("destinations are not to the right of the routes")
	}
	// The default route is evaluated last, so it is drawn last.
	if nodes["route:critical"].Y >= nodes["route:fallback"].Y {
		t.Error("the default route is not below the route that outranks it")
	}
	// Templates share the column with the destinations but sit below them.
	if nodes["template:tmpl"].X != nodes["destination:dest"].X {
		t.Error("templates are not in the same column as the destinations")
	}
	if nodes["template:tmpl"].Y <= nodes["destination:dest"].Y {
		t.Error("templates are not below the destinations")
	}
}

// The directory is a nicety: an unreachable Graph must still leave a drawable
// picture, with the ids the destination stores.
func TestRoutingGraphFallsBackToIdentifiers(t *testing.T) {
	t.Parallel()

	directory := &fakeMessenger{directoryErr: errors.New("graph is down")}
	nodes, _ := graphFrom(t, newTestServer(t, routingStore(), directory).Handler)

	if got := nodes["destination:dest"].Detail; got != "team › chan" {
		t.Errorf("destination detail = %q, want the raw ids", got)
	}
}

func TestRoutingGraphReportsStoreFailures(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"ListRoutes", "ListDestinations", "ListTemplates"} {
		st := routingStore().fail(method)
		rec := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/api/routing/graph", "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("with %s failing, graph = %d, want 500", method, rec.Code)
		}
	}
}

func TestRoutingMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		store           func() *fakeStore
		labels          string
		wantReason      string
		wantRoute       string
		wantExplanation string
	}{
		{
			name: "a selector matches", store: routingStore,
			labels: `{"severity":"critical"}`, wantReason: "selector",
			wantRoute: "route:critical", wantExplanation: "matched on severity=critical",
		},
		{
			name: "the default takes it", store: routingStore,
			labels: `{"severity":"warning"}`, wantReason: "default",
			wantRoute: "route:fallback", wantExplanation: "no selector matched",
		},
		{
			name: "no default to fall back on",
			store: func() *fakeStore {
				st := routingStore()
				delete(st.routes, "fallback")
				return st
			},
			labels: `{"severity":"warning"}`, wantReason: "none",
			wantExplanation: "would be rejected",
		},
		{
			name:   "nothing configured",
			store:  newFakeStore,
			labels: `{"severity":"critical"}`, wantReason: "no-routes",
			wantExplanation: "no routes are configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, tt.store(), &fakeMessenger{}).Handler
			rec := postJSON(t, handler, "/api/routing/match", `{"labels":`+tt.labels+`}`)

			if rec.Code != http.StatusOK {
				t.Fatalf("match = %d, want 200 (%s)", rec.Code, rec.Body.String())
			}

			var answer struct {
				Reason      string         `json:"reason"`
				Explanation string         `json:"explanation"`
				Route       map[string]any `json:"route"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if answer.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", answer.Reason, tt.wantReason)
			}
			if !strings.Contains(answer.Explanation, tt.wantExplanation) {
				t.Errorf("explanation = %q, want it to mention %q", answer.Explanation, tt.wantExplanation)
			}
			if tt.wantRoute == "" {
				if answer.Route != nil {
					t.Errorf("route = %v, want none when nothing was chosen", answer.Route)
				}
				return
			}
			if answer.Route["node"] != tt.wantRoute {
				t.Errorf("route node = %v, want %q so the graph can highlight it", answer.Route["node"], tt.wantRoute)
			}
		})
	}
}

func TestRoutingMatchRejectsBadRequests(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, routingStore(), &fakeMessenger{}).Handler

	if rec := postJSON(t, handler, "/api/routing/match", "not json"); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON = %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/routing/match", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/routing/graph", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST graph = %d, want 405", rec.Code)
	}
}

func TestRoutingPageRenders(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, routingStore(), &fakeMessenger{}).Handler
	rec := do(t, handler, http.MethodGet, "/admin/routing", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/routing = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`id="routing-graph"`, `id="match-form"`, `id="match-result"`,
		`name="labels"`, `src="/vendor/d3.min.js"`, `src="/routing.js"`,
		`href="/admin/routing"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the routing page is missing %q", want)
		}
	}
}

func TestRoutingPageNeedsASession(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, routingStore(), &fakeMessenger{}).Handler
	req := httptest.NewRequest(http.MethodGet, "/admin/routing", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("anonymous GET /admin/routing = %d, want a redirect to the login page", rec.Code)
	}
}

func postJSON(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "pass")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
