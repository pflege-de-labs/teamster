package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// twoDestinations is seededUIStore with a second destination and the first
// marked as the global default.
func twoDestinations() *fakeStore {
	st := seededUIStore()
	dest := st.destinations["dest"]
	dest.IsDefault = true
	st.destinations["dest"] = dest
	st.destinations["other"] = models.Destination{ID: "other", Name: "Other channel", TeamID: "team", ChannelID: "other"}
	return st
}

func TestSwitchingTheGlobalDefault(t *testing.T) {
	t.Parallel()

	sameOrigin := map[string]string{"Sec-Fetch-Site": "same-origin"}
	tests := []struct {
		name        string
		post        func(t *testing.T, handler http.Handler) int
		wantStatus  int
		wantDefault string
	}{
		{
			name: "the form",
			post: func(t *testing.T, handler http.Handler) int {
				return postForm(t, handler, "/admin/destinations/default", url.Values{"id": {"other"}}, sameOrigin).Code
			},
			wantStatus: http.StatusSeeOther, wantDefault: "other",
		},
		{
			name: "the API",
			post: func(t *testing.T, handler http.Handler) int {
				return do(t, handler, http.MethodPost, "/api/destinations/other/default", "").Code
			},
			wantStatus: http.StatusOK, wantDefault: "other",
		},
		{
			name: "the API, naming nothing",
			post: func(t *testing.T, handler http.Handler) int {
				return do(t, handler, http.MethodPost, "/api/destinations/missing/default", "").Code
			},
			wantStatus: http.StatusNotFound, wantDefault: "dest",
		},
		{
			name: "the API, by GET",
			post: func(t *testing.T, handler http.Handler) int {
				return do(t, handler, http.MethodGet, "/api/destinations/other/default", "").Code
			},
			wantStatus: http.StatusMethodNotAllowed, wantDefault: "dest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := twoDestinations()
			if got := tt.post(t, newTestServer(t, st, &fakeMessenger{}).Handler); got != tt.wantStatus {
				t.Errorf("status = %d, want %d", got, tt.wantStatus)
			}
			if got, _ := st.GetDefaultDestination(t.Context()); got.ID != tt.wantDefault {
				t.Errorf("default = %q, want %q", got.ID, tt.wantDefault)
			}
		})
	}
}

func TestAnEditorMayNotSwitchTheGlobalDefault(t *testing.T) {
	t.Parallel()

	st := sessionAs(twoDestinations(), authz.RoleEditor)
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/destinations/other/default", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestDeletingTheGlobalDefaultIsRefused(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, twoDestinations(), &fakeMessenger{}).Handler

	if rec := do(t, handler, http.MethodDelete, "/api/destinations/dest", ""); rec.Code != http.StatusConflict {
		t.Errorf("API delete = %d, want 409", rec.Code)
	}
	rec := postForm(t, handler, "/admin/destinations/delete", url.Values{"id": {"dest"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if location := rec.Header().Get("Location"); !strings.Contains(location, "error=") {
		t.Errorf("form delete redirected to %q, want an error", location)
	}
}

func TestAdminPageShowsTheGlobalDefault(t *testing.T) {
	t.Parallel()

	body := do(t, newTestServer(t, twoDestinations(), &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	for _, want := range []string{
		"global default",
		`action="/admin/destinations/default"`,
		`data-synthetic-route="global-default"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	// Only the destinations that are not already the default offer the switch.
	if got := strings.Count(body, `action="/admin/destinations/default"`); got != 1 {
		t.Errorf("switch forms = %d, want 1", got)
	}
}

func TestAdminPageOmitsTheSyntheticRouteWithoutADefault(t *testing.T) {
	t.Parallel()

	body := do(t, newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	if strings.Contains(body, `data-synthetic-route`) {
		t.Error("page draws a global default route though no destination is the default")
	}
}

func TestAdminPageReportsAGlobalDefaultReadFailure(t *testing.T) {
	t.Parallel()

	st := twoDestinations().fail("GetDefaultDestination")
	body := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	if strings.Contains(body, `data-synthetic-route`) {
		t.Error("page draws a global default it could not read")
	}
}
