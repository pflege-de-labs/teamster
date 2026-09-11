package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
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
	// A template is not a place an alert goes, so it is a label on the route
	// rather than a node in the flow.
	if got := nodes["route:critical"]; got.Template != "Critical card" || got.TemplateInherited {
		t.Errorf("route node = %+v, want the template it renders with as its own", got)
	}
	if _, ok := nodes["template:tmpl"]; ok {
		t.Error("the flow still has a template node, so an edge means two things")
	}
	if !nodes["route:fallback"].Default {
		t.Error("the default route is not marked as one")
	}

	// The webhook into each of two routes, and each route into a destination.
	if len(links) != 4 {
		t.Errorf("links = %d, want 4", len(links))
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
		`id="routing-graph"`, `id="template-graph"`, `id="match-form"`, `id="match-result"`,
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

// nestedRoutingStore is the tree the graph and the match probe have to describe:
// a parent, a child refining it, and a default.
func nestedRoutingStore() *fakeStore {
	st := routingStore()
	st.destinations["escalation"] = models.Destination{ID: "escalation", Name: "Escalation", TeamID: "team", ChannelID: "escalation-channel"}
	st.routes["child"] = models.Route{
		ID: "child", Name: "Payments escalation", ParentID: "critical",
		LabelSelector: map[string]string{"team": "payments"}, DestinationID: "escalation",
	}
	return st
}

// A child hangs off the route it refines, not off the webhook, because that is
// the order the router reaches it in.
func TestRoutingGraphNestsChildRoutes(t *testing.T) {
	t.Parallel()

	nodes, links := graphFrom(t, newTestServer(t, nestedRoutingStore(), &fakeMessenger{}).Handler)

	parents := map[string]string{}
	for _, link := range links {
		if strings.HasPrefix(link.Target, "route:") {
			parents[link.Target] = link.Source
		}
	}
	if got := parents["route:child"]; got != "route:critical" {
		t.Errorf("child is fed by %q, want its parent route", got)
	}
	if got := parents["route:critical"]; got != "source:webhook" {
		t.Errorf("root is fed by %q, want the webhook", got)
	}
	if nodes["route:child"].X <= nodes["route:critical"].X {
		t.Error("the child is not drawn to the right of its parent")
	}
}

// The child sets no template, so the picture has to name the one it inherits
// and say that it is inherited.
func TestRoutingGraphDrawsInheritedTargets(t *testing.T) {
	t.Parallel()

	nodes, links := graphFrom(t, newTestServer(t, nestedRoutingStore(), &fakeMessenger{}).Handler)

	targets := map[string]bool{}
	for _, link := range links {
		if link.Source == "route:child" {
			targets[link.Target] = true
		}
	}
	if !targets["destination:escalation"] {
		t.Error("the child does not reach its own destination")
	}

	child := nodes["route:child"]
	if child.Template != "Critical card" || !child.TemplateInherited {
		t.Errorf("child node = %+v, want the inherited template named and marked", child)
	}
}

// One kind of edge means one thing. Which step it is has to be readable, or the
// drawing cannot say whether a child delivers as well as its parent.
func TestRoutingGraphLabelsItsEdges(t *testing.T) {
	t.Parallel()

	st := nestedRoutingStore()
	greedy := st.routes["child"]
	greedy.Greedy = true
	st.routes["child"] = greedy

	_, links := graphFrom(t, newTestServer(t, st, &fakeMessenger{}).Handler)

	kinds := map[string]graphLink{}
	for _, link := range links {
		kinds[link.Source+">"+link.Target] = link
	}

	if got := kinds["source:webhook>route:critical"]; got.Kind != "enters" {
		t.Errorf("webhook edge = %+v, want it marked as entering", got)
	}
	if got := kinds["route:critical>route:child"]; got.Kind != "refines" || !got.Greedy {
		t.Errorf("parent edge = %+v, want it marked as a greedy refinement", got)
	}
	if got := kinds["route:child>destination:escalation"]; got.Kind != "delivers" {
		t.Errorf("destination edge = %+v, want it marked as delivering", got)
	}
	// A greedy child does not stop its parent from being drawn as delivering
	// when another alert does not match the child.
	if got := kinds["route:critical>destination:dest"]; got.Kind != "delivers" {
		t.Errorf("parent delivery edge = %+v, want it kept", got)
	}
}

func templateGraphFrom(t *testing.T, handler http.Handler) (map[string]graphNode, []graphLink) {
	t.Helper()

	rec := do(t, handler, http.MethodGet, "/api/routing/templates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET template graph = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Nodes []graphNode `json:"nodes"`
		Links []graphLink `json:"links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode template graph: %v", err)
	}

	byID := map[string]graphNode{}
	for _, node := range payload.Nodes {
		byID[node.ID] = node
	}
	return byID, payload.Links
}

// "Renders with" is not a step an alert takes, so it gets its own picture — and
// that picture is where a template nothing uses becomes visible.
func TestTemplateGraph(t *testing.T) {
	t.Parallel()

	st := nestedRoutingStore()
	st.templates["unused"] = models.Template{ID: "unused", Name: "Unused card"}

	nodes, links := templateGraphFrom(t, newTestServer(t, st, &fakeMessenger{}).Handler)

	used := map[string][]string{}
	for _, link := range links {
		if link.Kind != "renders" {
			t.Errorf("link %+v, want every edge to mean rendering", link)
		}
		used[link.Source] = append(used[link.Source], link.Target)
	}

	if len(used["template:tmpl"]) != 3 {
		t.Errorf("template:tmpl is used by %v, want all three routes including the one that inherits it", used["template:tmpl"])
	}
	if len(used["template:unused"]) != 0 {
		t.Errorf("template:unused is used by %v, want nothing", used["template:unused"])
	}
	if got := nodes["template:unused"]; got.Detail == "" {
		t.Errorf("unused template = %+v, want it to say no route renders with it", got)
	}
	if nodes["template:tmpl"].X >= nodes["route:critical"].X {
		t.Error("templates are not drawn left of the routes that use them")
	}
}

// A route rendering with a deleted template shows up here, since the flow graph
// no longer has a node for it.
func TestTemplateGraphMarksAMissingTemplate(t *testing.T) {
	t.Parallel()

	st := routingStore()
	delete(st.templates, "tmpl")

	nodes, links := templateGraphFrom(t, newTestServer(t, st, &fakeMessenger{}).Handler)

	missing, ok := nodes["template:tmpl"]
	if !ok || !missing.Missing {
		t.Fatalf("nodes = %v, want the deleted template marked missing", nodes)
	}
	if len(links) == 0 {
		t.Error("the routes rendering with the deleted template lost their edges")
	}
}

func TestTemplateGraphRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, routingStore(), &fakeMessenger{}).Handler
	if rec := do(t, handler, http.MethodPost, "/api/routing/templates", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST template graph = %d, want 405", rec.Code)
	}
}

func TestTemplateGraphReportsStoreFailures(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"ListRoutes", "ListTemplates"} {
		st := routingStore().fail(method)
		rec := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/api/routing/templates", "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("with %s failing, template graph = %d, want 500", method, rec.Code)
		}
	}
}

func TestRoutingMatchAnswersWithThePlan(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, nestedRoutingStore(), &fakeMessenger{}).Handler
	rec := do(t, handler, http.MethodPost, "/api/routing/match", `{"labels":{"severity":"critical","team":"payments"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("match = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Reason      string   `json:"reason"`
		Explanation string   `json:"explanation"`
		Nodes       []string `json:"nodes"`
		Deliveries  []struct {
			Route         map[string]any `json:"route"`
			DestinationID string         `json:"destination_id"`
			TemplateID    string         `json:"template_id"`
			Reason        string         `json:"reason"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode match: %v", err)
	}

	if len(payload.Deliveries) != 2 {
		t.Fatalf("deliveries = %+v, want the parent and the child", payload.Deliveries)
	}
	if payload.Deliveries[1].Reason != string(routing.ReasonRefined) {
		t.Errorf("child reason = %q, want %q", payload.Deliveries[1].Reason, routing.ReasonRefined)
	}
	if payload.Deliveries[1].TemplateID != "tmpl" {
		t.Errorf("child template = %q, want the inherited one", payload.Deliveries[1].TemplateID)
	}
	if !strings.Contains(payload.Explanation, "2 messages") {
		t.Errorf("explanation = %q, want it to say how many messages go out", payload.Explanation)
	}
	// The highlight follows every node on the path, not just the first route.
	for _, want := range []string{"route:critical", "route:child", "destination:dest", "destination:escalation"} {
		if !slices.Contains(payload.Nodes, want) {
			t.Errorf("nodes = %v, want %q among them", payload.Nodes, want)
		}
	}
}

// A greedy child means the route that matched first does not deliver, and the
// explanation has to say so rather than naming it as the destination.
func TestRoutingMatchExplainsAGreedyChild(t *testing.T) {
	t.Parallel()

	st := nestedRoutingStore()
	child := st.routes["child"]
	child.Greedy = true
	st.routes["child"] = child

	rec := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/routing/match",
		`{"labels":{"severity":"critical","team":"payments"}}`)

	var payload struct {
		Explanation string `json:"explanation"`
		Deliveries  []any  `json:"deliveries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode match: %v", err)
	}
	if len(payload.Deliveries) != 1 {
		t.Fatalf("deliveries = %d, want only the child's", len(payload.Deliveries))
	}
	if !strings.Contains(payload.Explanation, "instead of it") {
		t.Errorf("explanation = %q, want it to say the child delivers instead of its parent", payload.Explanation)
	}
}
