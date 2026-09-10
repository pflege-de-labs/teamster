package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	nodes, links := graphFrom(t, newTestServer(t, routingStore(), &fakeMessenger{}).Handler)

	if got := nodes["route:critical"]; got.Kind != "route" || got.Label != "Critical to ops" || got.Selector != "severity=critical" || got.Priority != 100 {
		t.Errorf("route node = %+v, want the route resolved with its selector", got)
	}
	if got := nodes["destination:dest"]; got.Kind != "destination" || !strings.Contains(got.Detail, "team") {
		t.Errorf("destination node = %+v, want the team and channel in the detail", got)
	}
	if got := nodes["template:tmpl"]; got.Label != "Critical card" {
		t.Errorf("template node = %+v, want the template name", got)
	}
	if !nodes["route:fallback"].Default {
		t.Error("the default route is not marked as one")
	}

	// Two routes, each pointing at a destination and a template.
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
	if len(nodes) != 0 || len(links) != 0 {
		t.Errorf("graph = %v / %v, want both empty rather than null", nodes, links)
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
