package httpserver

import (
	"net/http"
	"strings"
	"testing"
)

func TestHandleAdminServesTheUI(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	tests := []struct {
		name     string
		path     string
		wantBody string
	}{
		{name: "admin path serves index", path: "/admin", wantBody: "<html"},
		{name: "root serves index", path: "/", wantBody: "<html"},
		{name: "static asset", path: "/app.js", wantBody: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, handler, http.MethodGet, tt.path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}
			if tt.wantBody != "" && !strings.Contains(strings.ToLower(rec.Body.String()), tt.wantBody) {
				t.Errorf("GET %s body does not contain %q", tt.path, tt.wantBody)
			}
		})
	}
}

func TestUnknownAssetIsNotFound(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler, http.MethodGet, "/nope.js", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /nope.js = %d, want 404", rec.Code)
	}
}
