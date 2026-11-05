package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func linkedStore() *fakeStore {
	st := webhookUIStore()
	route := endpointRoute(st.webhooks["hook"])
	route.ID = "wroute"
	st.routes["wroute"] = route
	return st
}

func routeFor(st *fakeStore, endpoint string) (models.Route, bool) {
	for _, r := range st.routes {
		if r.WebhookEndpointID == endpoint {
			return r, true
		}
	}
	return models.Route{}, false
}

func TestEndpointRouteSync(t *testing.T) {
	t.Parallel()

	type body struct{ team, channel, dest, tmpl string }
	tests := []struct {
		name   string
		setup  func(*fakeStore)
		method string // POST creates, PUT updates, DELETE deletes
		in     body
		check  func(t *testing.T, st *fakeStore, code int)
	}{
		{
			name: "create makes linked route", method: http.MethodPost,
			in: body{"platform", "alerts", "dest", "tmpl"},
			check: func(t *testing.T, st *fakeStore, _ int) {
				r, ok := routeFor(st, "generated")
				if !ok {
					t.Fatal("no linked route")
				}
				if r.Name != "webhook platform/alerts" || r.DestinationID != "dest" || r.TemplateID != "tmpl" ||
					r.LabelSelector[models.TeamLabel] != "platform" || r.LabelSelector[models.ChannelLabel] != "alerts" ||
					r.LabelSelector[models.SourceLabel] != models.SourceTeamsV2 || r.Priority != 0 || r.Greedy || r.IsDefault {
					t.Errorf("route = %+v", r)
				}
			},
		},
		{
			name: "update rewrites selector", method: http.MethodPut, setup: func(st *fakeStore) {
				r := st.routes["wroute"]
				r.Priority, r.Greedy = 7, true
				st.routes["wroute"] = r
			},
			in: body{"ops", "pager", "dest", ""},
			check: func(t *testing.T, st *fakeStore, _ int) {
				r := st.routes["wroute"]
				if r.LabelSelector[models.TeamLabel] != "ops" || r.LabelSelector[models.ChannelLabel] != "pager" || r.Name != "webhook ops/pager" {
					t.Errorf("route = %+v", r)
				}
				if r.Priority != 7 || !r.Greedy {
					t.Errorf("priority/greedy lost: %+v", r)
				}
			},
		},
		{
			name: "update keeps an edited name", method: http.MethodPut, setup: func(st *fakeStore) {
				r := st.routes["wroute"]
				r.Name = "my pager"
				st.routes["wroute"] = r
			},
			in: body{"ops", "pager", "dest", ""},
			check: func(t *testing.T, st *fakeStore, _ int) {
				if got := st.routes["wroute"].Name; got != "my pager" {
					t.Errorf("name = %q", got)
				}
			},
		},
		{
			name: "update changes destination and template", method: http.MethodPut, setup: func(st *fakeStore) {
				st.destinations["dest2"] = models.Destination{ID: "dest2", Name: "Other", TeamID: "t", ChannelID: "c"}
			},
			in: body{"platform", "alerts", "dest2", "tmpl"},
			check: func(t *testing.T, st *fakeStore, _ int) {
				if r := st.routes["wroute"]; r.DestinationID != "dest2" || r.TemplateID != "tmpl" {
					t.Errorf("route = %+v", r)
				}
			},
		},
		{
			name: "update recreates a missing route", method: http.MethodPut, setup: func(st *fakeStore) { delete(st.routes, "wroute") },
			in: body{"platform", "alerts", "dest", ""},
			check: func(t *testing.T, st *fakeStore, _ int) {
				if _, ok := routeFor(st, "hook"); !ok {
					t.Error("route not recreated")
				}
			},
		},
		{
			name: "delete removes route and its permissions", method: http.MethodDelete, setup: func(st *fakeStore) {
				st.permissions["p"] = models.Permission{ID: "p", ResourceType: typeRoute, ResourceID: "wroute"}
			},
			check: func(t *testing.T, st *fakeStore, _ int) {
				if _, ok := st.routes["wroute"]; ok {
					t.Error("route still present")
				}
				if _, ok := st.permissions["p"]; ok {
					t.Error("route permission still present")
				}
				if _, ok := st.webhooks["hook"]; ok {
					t.Error("endpoint still present")
				}
			},
		},
		{
			name: "delete refused while route has children", method: http.MethodDelete, setup: func(st *fakeStore) {
				st.routes["child"] = models.Route{ID: "child", Name: "child", ParentID: "wroute", DestinationID: "dest"}
			},
			check: func(t *testing.T, st *fakeStore, code int) {
				if _, ok := st.webhooks["hook"]; !ok {
					t.Error("endpoint was deleted")
				}
				if _, ok := st.routes["wroute"]; !ok {
					t.Error("route was deleted")
				}
				if code != http.StatusBadRequest && code != http.StatusSeeOther {
					t.Errorf("status = %d", code)
				}
			},
		},
	}

	for _, tt := range tests {
		for _, via := range []string{"form", "api"} {
			t.Run(tt.name+"/"+via, func(t *testing.T) {
				t.Parallel()

				st := linkedStore()
				if tt.setup != nil {
					tt.setup(st)
				}
				handler := newTestServer(t, st, &fakeMessenger{}).Handler
				var rec *httptest.ResponseRecorder
				hdr := map[string]string{"Sec-Fetch-Site": "same-origin"}
				switch {
				case via == "form" && tt.method == http.MethodDelete:
					rec = postForm(t, handler, "/admin/webhooks/delete", url.Values{"id": {"hook"}}, hdr)
				case via == "form":
					form := url.Values{"team_slug": {tt.in.team}, "channel_slug": {tt.in.channel}, "destination_id": {tt.in.dest}, "template_id": {tt.in.tmpl}}
					if tt.method == http.MethodPut {
						form.Set("id", "hook")
					}
					rec = postForm(t, handler, "/admin/webhooks", form, hdr)
				case tt.method == http.MethodDelete:
					rec = do(t, handler, tt.method, "/api/webhooks/hook", "")
				case tt.method == http.MethodPut:
					rec = do(t, handler, tt.method, "/api/webhooks/hook", `{"team_slug":"`+tt.in.team+`","channel_slug":"`+tt.in.channel+`","destination_id":"`+tt.in.dest+`","template_id":"`+tt.in.tmpl+`"}`)
				default:
					rec = postJSON(t, handler, "/api/webhooks", `{"team_slug":"`+tt.in.team+`","channel_slug":"`+tt.in.channel+`","destination_id":"`+tt.in.dest+`","template_id":"`+tt.in.tmpl+`"}`)
				}
				if tt.name != "delete refused while route has children" && rec.Code >= 400 {
					t.Fatalf("status = %d: %s", rec.Code, rec.Body)
				}
				if tt.name == "delete refused while route has children" && via == "form" &&
					!strings.Contains(rec.Header().Get("Location"), "child") {
					t.Errorf("redirect carries no notice: %s", rec.Header().Get("Location"))
				}
				tt.check(t, st, rec.Code)
			})
		}
	}
}

func TestSyncEndpointRouteGrantsOwner(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), subjectKey, "alice"))
	if err := syncEndpointRoute(req.Context(), st, req, st.webhooks["hook"]); err != nil {
		t.Fatal(err)
	}
	route, _ := routeFor(st, "hook")
	for _, p := range st.permissions {
		if p.ResourceType == typeRoute && p.ResourceID == route.ID && p.PrincipalID == "alice" {
			return
		}
	}
	t.Error("no owner permission on the new route")
}
