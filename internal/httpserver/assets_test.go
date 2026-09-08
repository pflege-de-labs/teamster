package httpserver

import (
	"net/http"
	"regexp"
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

func TestHandleAdminServesTheIcons(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	tests := []struct {
		name            string
		path            string
		wantContentType string
	}{
		{name: "favicon at the root, where browsers look for it", path: "/favicon.ico", wantContentType: "image/x-icon"},
		{name: "16px png", path: "/icons/favicon-16x16.png", wantContentType: "image/png"},
		{name: "32px png", path: "/icons/favicon-32x32.png", wantContentType: "image/png"},
		{name: "header logo", path: "/icons/favicon-48x48.png", wantContentType: "image/png"},
		{name: "apple touch icon", path: "/icons/apple-touch-icon.png", wantContentType: "image/png"},
		{name: "maskable 192", path: "/icons/icon-192-maskable.png", wantContentType: "image/png"},
		{name: "maskable 512", path: "/icons/icon-512-maskable.png", wantContentType: "image/png"},
		{name: "web manifest", path: "/site.webmanifest", wantContentType: "application/manifest+json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, handler, http.MethodGet, tt.path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.wantContentType) {
				t.Errorf("GET %s Content-Type = %q, want %q", tt.path, got, tt.wantContentType)
			}
		})
	}
}

// Every icon the page asks for has to exist, or the browser logs a 404.
func TestIndexReferencesOnlyExistingAssets(t *testing.T) {
	t.Parallel()

	page, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}

	refs := regexp.MustCompile(`(?:href|src)="(/[^"]+)"`).FindAllStringSubmatch(string(page), -1)
	if len(refs) == 0 {
		t.Fatal("index.html references no assets, the test would prove nothing")
	}

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	for _, ref := range refs {
		path := ref[1]
		if rec := do(t, handler, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("index.html references %s, which returns %d", path, rec.Code)
		}
	}
}
