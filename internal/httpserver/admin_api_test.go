package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func newTestServer(t *testing.T, st *fakeStore, msg *fakeMessenger) *http.Server {
	t.Helper()

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	return NewServer(cfg, st, msg)
}

func do(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.SetBasicAuth("admin", "pass")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminCollectionEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		failOn     string
		wantStatus int
	}{
		{name: "list templates", method: http.MethodGet, path: "/api/templates", wantStatus: http.StatusOK},
		{name: "list templates fails", method: http.MethodGet, path: "/api/templates", failOn: "ListTemplates", wantStatus: http.StatusInternalServerError},
		{name: "create template", method: http.MethodPost, path: "/api/templates", body: `{"name":"card"}`, wantStatus: http.StatusCreated},
		{name: "create template invalid JSON", method: http.MethodPost, path: "/api/templates", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "create template fails", method: http.MethodPost, path: "/api/templates", body: `{}`, failOn: "CreateTemplate", wantStatus: http.StatusInternalServerError},
		{name: "templates reject PATCH", method: http.MethodPatch, path: "/api/templates", wantStatus: http.StatusMethodNotAllowed},

		{name: "list destinations", method: http.MethodGet, path: "/api/destinations", wantStatus: http.StatusOK},
		{name: "list destinations fails", method: http.MethodGet, path: "/api/destinations", failOn: "ListDestinations", wantStatus: http.StatusInternalServerError},
		{name: "create destination", method: http.MethodPost, path: "/api/destinations", body: `{"name":"ops"}`, wantStatus: http.StatusCreated},
		{name: "create destination invalid JSON", method: http.MethodPost, path: "/api/destinations", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "create destination fails", method: http.MethodPost, path: "/api/destinations", body: `{}`, failOn: "CreateDestination", wantStatus: http.StatusInternalServerError},
		{name: "destinations reject PATCH", method: http.MethodPatch, path: "/api/destinations", wantStatus: http.StatusMethodNotAllowed},

		{name: "list routes", method: http.MethodGet, path: "/api/routes", wantStatus: http.StatusOK},
		{name: "list routes fails", method: http.MethodGet, path: "/api/routes", failOn: "ListRoutes", wantStatus: http.StatusInternalServerError},
		{name: "create route", method: http.MethodPost, path: "/api/routes", body: `{"name":"critical"}`, wantStatus: http.StatusCreated},
		{name: "create route invalid JSON", method: http.MethodPost, path: "/api/routes", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "create route fails", method: http.MethodPost, path: "/api/routes", body: `{}`, failOn: "CreateRoute", wantStatus: http.StatusInternalServerError},
		{name: "routes reject PATCH", method: http.MethodPatch, path: "/api/routes", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			if tt.failOn != "" {
				st.fail(tt.failOn)
			}

			rec := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s = %d, want %d (body %s)", tt.method, tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestAdminItemEndpoints(t *testing.T) {
	t.Parallel()

	seed := func() *fakeStore {
		st := newFakeStore()
		st.templates["known"] = models.Template{ID: "known", Name: "card"}
		st.destinations["known"] = models.Destination{ID: "known", Name: "ops"}
		st.routes["known"] = models.Route{ID: "known", Name: "critical"}
		return st
	}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		failOn     string
		wantStatus int
	}{
		{name: "get template", method: http.MethodGet, path: "/api/templates/known", wantStatus: http.StatusOK},
		{name: "get unknown template", method: http.MethodGet, path: "/api/templates/missing", wantStatus: http.StatusNotFound},
		{name: "get template fails", method: http.MethodGet, path: "/api/templates/known", failOn: "GetTemplate", wantStatus: http.StatusInternalServerError},
		{name: "update template", method: http.MethodPut, path: "/api/templates/known", body: `{"name":"new"}`, wantStatus: http.StatusOK},
		{name: "update template invalid JSON", method: http.MethodPut, path: "/api/templates/known", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "update template fails", method: http.MethodPut, path: "/api/templates/known", body: `{}`, failOn: "UpdateTemplate", wantStatus: http.StatusInternalServerError},
		{name: "delete template", method: http.MethodDelete, path: "/api/templates/known", wantStatus: http.StatusOK},
		{name: "delete template fails", method: http.MethodDelete, path: "/api/templates/known", failOn: "DeleteTemplate", wantStatus: http.StatusInternalServerError},
		{name: "template id required", method: http.MethodGet, path: "/api/templates/", wantStatus: http.StatusNotFound},
		{name: "template rejects PATCH", method: http.MethodPatch, path: "/api/templates/known", wantStatus: http.StatusMethodNotAllowed},

		{name: "get destination", method: http.MethodGet, path: "/api/destinations/known", wantStatus: http.StatusOK},
		{name: "get unknown destination", method: http.MethodGet, path: "/api/destinations/missing", wantStatus: http.StatusNotFound},
		{name: "get destination fails", method: http.MethodGet, path: "/api/destinations/known", failOn: "GetDestination", wantStatus: http.StatusInternalServerError},
		{name: "update destination", method: http.MethodPut, path: "/api/destinations/known", body: `{"name":"new"}`, wantStatus: http.StatusOK},
		{name: "update destination invalid JSON", method: http.MethodPut, path: "/api/destinations/known", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "update destination fails", method: http.MethodPut, path: "/api/destinations/known", body: `{}`, failOn: "UpdateDestination", wantStatus: http.StatusInternalServerError},
		{name: "delete destination", method: http.MethodDelete, path: "/api/destinations/known", wantStatus: http.StatusOK},
		{name: "delete destination fails", method: http.MethodDelete, path: "/api/destinations/known", failOn: "DeleteDestination", wantStatus: http.StatusInternalServerError},
		{name: "destination id required", method: http.MethodGet, path: "/api/destinations/", wantStatus: http.StatusNotFound},
		{name: "destination rejects PATCH", method: http.MethodPatch, path: "/api/destinations/known", wantStatus: http.StatusMethodNotAllowed},

		{name: "get route", method: http.MethodGet, path: "/api/routes/known", wantStatus: http.StatusOK},
		{name: "get unknown route", method: http.MethodGet, path: "/api/routes/missing", wantStatus: http.StatusNotFound},
		{name: "get route fails", method: http.MethodGet, path: "/api/routes/known", failOn: "GetRoute", wantStatus: http.StatusInternalServerError},
		{name: "update route", method: http.MethodPut, path: "/api/routes/known", body: `{"name":"new"}`, wantStatus: http.StatusOK},
		{name: "update route invalid JSON", method: http.MethodPut, path: "/api/routes/known", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "update route fails", method: http.MethodPut, path: "/api/routes/known", body: `{}`, failOn: "UpdateRoute", wantStatus: http.StatusInternalServerError},
		{name: "delete route", method: http.MethodDelete, path: "/api/routes/known", wantStatus: http.StatusOK},
		{name: "delete route fails", method: http.MethodDelete, path: "/api/routes/known", failOn: "DeleteRoute", wantStatus: http.StatusInternalServerError},
		{name: "route id required", method: http.MethodGet, path: "/api/routes/", wantStatus: http.StatusNotFound},
		{name: "route rejects PATCH", method: http.MethodPatch, path: "/api/routes/known", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seed()
			if tt.failOn != "" {
				st.fail(tt.failOn)
			}

			rec := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s = %d, want %d (body %s)", tt.method, tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestUpdateOverwritesIDFromPath(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.templates["known"] = models.Template{ID: "known"}

	rec := do(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPut, "/api/templates/known", `{"id":"spoofed","name":"card"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200", rec.Code)
	}
	if _, ok := st.templates["spoofed"]; ok {
		t.Error("the body ID overrode the path ID, want the path to win")
	}
	if st.templates["known"].Name != "card" {
		t.Errorf("template not updated: %+v", st.templates["known"])
	}
}
