package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
)

func TestAuthorizeFailsClosedWithoutTheGeneration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{"a page", "/admin"},
		{"the API", "/api/templates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(newFakeStore(), authz.RoleAdmin).fail("AuthzGeneration")
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			if rec := asRole(t, handler, http.MethodGet, tt.path, ""); rec.Code != http.StatusServiceUnavailable {
				t.Errorf("GET %s = %d, want 503", tt.path, rec.Code)
			}
		})
	}
}

func TestEveryRequestReadsTheCurrentGeneration(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	var seen []int64
	api := &Server{store: st}
	engine, err := authz.NewEngine(authzSource{st})
	if err != nil {
		t.Fatal(err)
	}
	api.engine = engine
	handler := api.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, api.policies(r).Generation())
	}))
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req = req.WithContext(withPrincipal(req.Context(), "tester", "tester", "session", []authz.Role{authz.RoleEditor}))
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if err := st.BumpAuthzGeneration(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 || seen[0] != 0 || seen[1] != 1 {
		t.Errorf("generations seen = %v, want [0 1]", seen)
	}
}
