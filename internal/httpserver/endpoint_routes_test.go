package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
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

func TestReconcileEndpointRoutes(t *testing.T) {
	t.Parallel()

	own := func(principal, resType, resID string) models.Permission {
		return models.Permission{
			PrincipalType: models.PrincipalUser, PrincipalID: principal,
			ResourceType: resType, ResourceID: resID,
			Actions: []string{authz.ActionOwn}, CreatedBy: "creator",
		}
	}
	put := func(st *fakeStore, ps ...models.Permission) {
		for _, p := range ps {
			if _, err := st.PutPermission(context.Background(), p); err != nil {
				panic(err)
			}
		}
	}
	owners := func(st *fakeStore, routeID string) []string {
		var out []string
		for _, p := range st.permissions {
			if p.ResourceType == typeRoute && p.ResourceID == routeID {
				if len(p.Actions) != 1 || p.Actions[0] != authz.ActionOwn || p.CreatedBy != "creator" {
					t.Errorf("permission = %+v", p)
				}
				out = append(out, p.PrincipalID)
			}
		}
		slices.Sort(out)
		return out
	}

	tests := []struct {
		name  string
		setup func(*fakeStore)
		want  Reconciled
		check func(t *testing.T, st *fakeStore)
	}{
		{
			name: "backfill copies owners",
			setup: func(st *fakeStore) {
				put(st, own("alice", typeWebhookEndpoint, "hook"), own("bob", typeWebhookEndpoint, "hook"),
					models.Permission{
						PrincipalType: models.PrincipalUser, PrincipalID: "carol",
						ResourceType: typeWebhookEndpoint, ResourceID: "hook", Actions: []string{authz.ActionRead},
					})
			},
			want: Reconciled{Created: 1},
			check: func(t *testing.T, st *fakeStore) {
				r, ok := routeFor(st, "hook")
				if !ok {
					t.Fatal("no linked route")
				}
				if got := owners(st, r.ID); !slices.Equal(got, []string{"alice", "bob"}) {
					t.Errorf("owners = %v", got)
				}
			},
		},
		{
			name:  "repair after route deleted",
			setup: func(st *fakeStore) { put(st, own("alice", typeWebhookEndpoint, "hook")) },
			want:  Reconciled{Created: 1},
			check: func(t *testing.T, st *fakeStore) {
				if _, ok := routeFor(st, "hook"); !ok {
					t.Error("route not recreated")
				}
			},
		},
		{
			name: "orphan deleted",
			setup: func(st *fakeStore) {
				st.webhooks = map[string]models.WebhookEndpoint{}
				st.routes["gone"] = models.Route{ID: "gone", WebhookEndpointID: "missing"}
				put(st, own("alice", typeRoute, "gone"))
			},
			want: Reconciled{Deleted: 1},
			check: func(t *testing.T, st *fakeStore) {
				if len(st.routes) != 0 || len(st.permissions) != 0 {
					t.Errorf("routes = %v, permissions = %v", st.routes, st.permissions)
				}
			},
		},
		{
			name: "orphan with children unlinked",
			setup: func(st *fakeStore) {
				st.webhooks = map[string]models.WebhookEndpoint{}
				st.routes["gone"] = models.Route{ID: "gone", WebhookEndpointID: "missing"}
				st.routes["kid"] = models.Route{ID: "kid", ParentID: "gone"}
				put(st, own("alice", typeRoute, "gone"))
			},
			want: Reconciled{Unlinked: 1},
			check: func(t *testing.T, st *fakeStore) {
				r, ok := st.routes["gone"]
				if !ok || r.WebhookEndpointID != "" {
					t.Errorf("route = %+v, kept = %v", r, ok)
				}
				if got := owners(st, "gone"); !slices.Equal(got, []string{"alice"}) {
					t.Errorf("owners = %v", got)
				}
			},
		},
		{
			name: "second run is a no-op",
			setup: func(st *fakeStore) {
				put(st, own("alice", typeWebhookEndpoint, "hook"))
				if _, err := ReconcileEndpointRoutes(context.Background(), st); err != nil {
					panic(err)
				}
			},
			want: Reconciled{},
			check: func(t *testing.T, st *fakeStore) {
				if len(st.routes) != 1 || len(st.permissions) != 2 {
					t.Errorf("routes = %v, permissions = %v", st.routes, st.permissions)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := webhookUIStore()
			st.routes = map[string]models.Route{}
			tt.setup(st)
			got, err := ReconcileEndpointRoutes(context.Background(), st)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Reconciled = %+v, want %+v", got, tt.want)
			}
			tt.check(t, st)
		})
	}
}
