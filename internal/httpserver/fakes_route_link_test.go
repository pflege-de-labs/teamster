package httpserver

import (
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestFakeStoreRouteLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, f *fakeStore)
	}{
		{"create keeps the link", func(t *testing.T, f *fakeStore) {
			r, err := f.CreateRoute(t.Context(), models.Route{ID: "a", WebhookEndpointID: "hook"})
			if err != nil || r.WebhookEndpointID != "hook" {
				t.Fatalf("CreateRoute = %+v, %v", r, err)
			}
			if got, _ := f.GetRoute(t.Context(), "a"); got.WebhookEndpointID != "hook" {
				t.Errorf("stored link = %q, want hook", got.WebhookEndpointID)
			}
		}},
		{"update preserves the link", func(t *testing.T, f *fakeStore) {
			_, _ = f.CreateRoute(t.Context(), models.Route{ID: "a", WebhookEndpointID: "hook"})
			if _, err := f.UpdateRoute(t.Context(), models.Route{ID: "a", Name: "renamed"}); err != nil {
				t.Fatal(err)
			}
			if got, _ := f.GetRoute(t.Context(), "a"); got.WebhookEndpointID != "hook" || got.Name != "renamed" {
				t.Errorf("route = %+v, want link kept and name updated", got)
			}
		}},
		{"duplicate link on create errors", func(t *testing.T, f *fakeStore) {
			_, _ = f.CreateRoute(t.Context(), models.Route{ID: "a", WebhookEndpointID: "hook"})
			if _, err := f.CreateRoute(t.Context(), models.Route{ID: "b", WebhookEndpointID: "hook"}); err == nil {
				t.Error("want an error")
			}
		}},
		{"unlink clears the link", func(t *testing.T, f *fakeStore) {
			_, _ = f.CreateRoute(t.Context(), models.Route{ID: "a", WebhookEndpointID: "hook"})
			if err := f.UnlinkRoute(t.Context(), "a"); err != nil {
				t.Fatal(err)
			}
			if got, _ := f.GetRoute(t.Context(), "a"); got.WebhookEndpointID != "" {
				t.Errorf("link = %q, want empty", got.WebhookEndpointID)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t, newFakeStore())
		})
	}
}
