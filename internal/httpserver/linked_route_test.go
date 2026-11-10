package httpserver

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

const managedDelete = "managed by webhook platform/alerts; delete the webhook instead"

func linkedRouteStore() *fakeStore {
	st := linkedStore()
	st.destinations["dest2"] = models.Destination{ID: "dest2", Name: "Other", TeamID: "t", ChannelID: "c"}
	st.templates["tmpl2"] = models.Template{ID: "tmpl2", Name: "Other card", Body: `{"type":"AdaptiveCard"}`}
	st.routes["root"] = models.Route{ID: "root", Name: "root", DestinationID: "dest", LabelSelector: map[string]string{"a": "b"}}
	return st
}

func routeForm(r models.Route) url.Values {
	target := ""
	switch {
	case r.Addressed:
		target = targetAddressed
	case r.RecipientID != "":
		target = targetRecipient + r.RecipientID
	case r.DestinationID != "":
		target = targetDestination + r.DestinationID
	}
	selector, _ := json.Marshal(r.LabelSelector)
	return url.Values{
		"id": {r.ID}, "name": {r.Name}, "priority": {strconv.Itoa(r.Priority)}, "parent_id": {r.ParentID},
		"label_selector": {string(selector)}, "target": {target}, "template_id": {r.TemplateID},
		"is_default": {strconv.FormatBool(r.IsDefault)}, "greedy": {strconv.FormatBool(r.Greedy)},
	}
}

// respText joins the body and the redirect, where a form post reports its error.
func respText(rec *httptest.ResponseRecorder) (int, string) {
	location, _ := url.QueryUnescape(rec.Header().Get("Location"))
	return rec.Code, rec.Body.String() + location
}

func TestLinkedRouteIsLocked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		edit   func(*models.Route)
		locked bool
	}{
		{"selector", func(r *models.Route) { r.LabelSelector = map[string]string{"x": "y"} }, true},
		{"destination", func(r *models.Route) { r.DestinationID = "dest2" }, true},
		{"recipient", func(r *models.Route) { r.DestinationID, r.RecipientID = "", "p1" }, true},
		{"addressed", func(r *models.Route) { r.DestinationID, r.Addressed = "", true }, true},
		{"template", func(r *models.Route) { r.TemplateID = "tmpl2" }, true},
		{"parent", func(r *models.Route) { r.ParentID = "root" }, true},
		{"default", func(r *models.Route) { r.IsDefault = true }, true},
		{"priority change accepted", func(r *models.Route) { r.Priority = 9 }, false},
		{"name change accepted", func(r *models.Route) { r.Name = "renamed" }, false},
		{"greedy change accepted", func(r *models.Route) { r.Greedy = true }, false},
		// The webhook owns the flag, so a client that leaves it out or clears it changes nothing.
		{"no greedy children cleared is ignored", func(r *models.Route) { r.NoGreedyChildren = false }, false},
	}
	for _, via := range []string{"api", "form"} {
		for _, tt := range tests {
			t.Run(via+" "+tt.name, func(t *testing.T) {
				t.Parallel()

				st := linkedRouteStore()
				before := st.routes["wroute"]
				r := before
				r.LabelSelector = maps.Clone(before.LabelSelector)
				tt.edit(&r)
				handler := newTestServer(t, st, &fakeMessenger{}).Handler

				var code int
				var body string
				if via == "api" {
					payload, _ := json.Marshal(r)
					code, body = respText(do(t, handler, http.MethodPut, "/api/routes/wroute", string(payload)))
				} else {
					code, body = respText(postForm(t, handler, "/admin/routes", routeForm(r), nil))
				}

				got := st.routes["wroute"]
				if tt.locked {
					if !strings.Contains(body, "managed by webhook platform/alerts") {
						t.Errorf("status %d, body %q, want the managed refusal", code, body)
					}
					if got.Name != before.Name || got.Priority != before.Priority || got.Greedy != before.Greedy ||
						got.DestinationID != before.DestinationID || got.TemplateID != before.TemplateID ||
						got.ParentID != before.ParentID || got.IsDefault != before.IsDefault ||
						got.RecipientID != before.RecipientID || got.Addressed != before.Addressed ||
						!maps.Equal(got.LabelSelector, before.LabelSelector) {
						t.Errorf("route changed: %+v", got)
					}
					return
				}
				if strings.Contains(body, "managed by webhook") {
					t.Fatalf("refused: %q", body)
				}
				if got.Name != r.Name || got.Priority != r.Priority || got.Greedy != r.Greedy || !got.NoGreedyChildren {
					t.Errorf("route = %+v, want the edit applied and the flag kept", got)
				}
			})
		}
	}
}

func TestLinkedRouteDeleteAndCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(st *fakeStore)
		run   func(t *testing.T, h http.Handler) string
		check func(t *testing.T, st *fakeStore, body string)
	}{
		{"delete refused via api", nil, func(t *testing.T, h http.Handler) string {
			return do(t, h, http.MethodDelete, "/api/routes/wroute", "").Body.String()
		}, func(t *testing.T, st *fakeStore, body string) {
			if !strings.Contains(body, managedDelete) {
				t.Errorf("body = %q", body)
			}
			if _, ok := st.routes["wroute"]; !ok {
				t.Error("route deleted")
			}
		}},
		{"delete refused via form", nil, func(t *testing.T, h http.Handler) string {
			_, body := respText(postForm(t, h, "/admin/routes/delete", url.Values{"id": {"wroute"}}, nil))
			return body
		}, func(t *testing.T, st *fakeStore, body string) {
			if !strings.Contains(body, managedDelete) {
				t.Errorf("body = %q", body)
			}
			if _, ok := st.routes["wroute"]; !ok {
				t.Error("route deleted")
			}
		}},
		{"delete allowed for an orphan", func(st *fakeStore) {
			delete(st.webhooks, "hook")
			st.permissions["p"] = models.Permission{ID: "p", ResourceType: typeRoute, ResourceID: "wroute"}
		}, func(t *testing.T, h http.Handler) string {
			return do(t, h, http.MethodDelete, "/api/routes/wroute", "").Body.String()
		}, func(t *testing.T, st *fakeStore, body string) {
			if _, ok := st.routes["wroute"]; ok {
				t.Errorf("route kept: %q", body)
			}
			if _, ok := st.permissions["p"]; ok {
				t.Error("route permission kept")
			}
		}},
		{"priority change accepted though the template is for other sources", func(st *fakeStore) {
			st.templates["am"] = models.Template{ID: "am", Name: "Alertmanager card", Body: `{"type":"AdaptiveCard"}`, Sources: []string{models.SourceAlertmanager}}
			r := st.routes["wroute"]
			r.TemplateID = "am"
			st.routes["wroute"] = r
		}, func(t *testing.T, h http.Handler) string {
			return do(t, h, http.MethodPut, "/api/routes/wroute",
				`{"name":"webhook platform/alerts","priority":4,"destination_id":"dest","template_id":"am",`+
					`"label_selector":{"teamster_source":"teamsv2","teamster_team":"platform","teamster_channel":"alerts"}}`).Body.String()
		}, func(t *testing.T, st *fakeStore, body string) {
			if st.routes["wroute"].Priority != 4 {
				t.Errorf("priority not saved: %q", body)
			}
		}},
		{"update response carries the link", nil, func(t *testing.T, h http.Handler) string {
			return do(t, h, http.MethodPut, "/api/routes/wroute",
				`{"name":"renamed","destination_id":"dest",`+
					`"label_selector":{"teamster_source":"teamsv2","teamster_team":"platform","teamster_channel":"alerts"}}`).Body.String()
		}, func(t *testing.T, _ *fakeStore, body string) {
			var got models.Route
			if err := json.Unmarshal([]byte(body), &got); err != nil || got.WebhookEndpointID != "hook" {
				t.Errorf("response = %q, want webhook_endpoint_id hook", body)
			}
		}},
		{"create cannot set webhook_endpoint_id", nil, func(t *testing.T, h http.Handler) string {
			return do(t, h, http.MethodPost, "/api/routes", `{"id":"","name":"mine","destination_id":"dest","webhook_endpoint_id":"hook"}`).Body.String()
		}, func(t *testing.T, st *fakeStore, _ string) {
			for _, r := range st.routes {
				if r.Name == "mine" && r.WebhookEndpointID != "" {
					t.Errorf("stored route has %q", r.WebhookEndpointID)
				}
			}
		}},
		{"child under linked route accepted", nil, func(t *testing.T, h http.Handler) string {
			return do(t, h, http.MethodPost, "/api/routes", `{"name":"child","parent_id":"wroute","template_id":"tmpl2","label_selector":{"severity":"critical"}}`).Body.String()
		}, func(t *testing.T, st *fakeStore, body string) {
			for _, r := range st.routes {
				if r.Name == "child" {
					return
				}
			}
			t.Errorf("child not created: %q", body)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := linkedRouteStore()
			if tt.setup != nil {
				tt.setup(st)
			}
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			tt.check(t, st, tt.run(t, handler))
		})
	}
}

func TestLinkedRouteForms(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, linkedRouteStore(), &fakeMessenger{}).Handler

	route := do(t, handler, http.MethodGet, "/admin/routes?edit=wroute", "").Body.String()
	for _, want := range []string{"Managed by webhook platform/alerts", `href="/admin/webhooks?edit=hook"`, "disabled"} {
		if !strings.Contains(route, want) {
			t.Errorf("route form lacks %q", want)
		}
	}
	endpoint := do(t, handler, http.MethodGet, "/admin/webhooks?edit=hook", "").Body.String()
	if !strings.Contains(endpoint, `href="/admin/routes?edit=wroute"`) {
		t.Error("endpoint form does not link to its route")
	}
}
