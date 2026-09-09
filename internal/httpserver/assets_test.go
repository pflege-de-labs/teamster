package httpserver

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestAdminPageAndAssets(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "admin page is rendered", path: "/admin", wantStatus: http.StatusOK, wantBody: "teamster admin"},
		{name: "root redirects to the admin page", path: "/", wantStatus: http.StatusFound},
		{name: "stylesheet is served", path: "/styles.css", wantStatus: http.StatusOK, wantBody: "tailwindcss"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && !strings.Contains(strings.ToLower(rec.Body.String()), tt.wantBody) {
				t.Errorf("GET %s body does not contain %q", tt.path, tt.wantBody)
			}
		})
	}
}

func TestRootRedirectsToAdmin(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler, http.MethodGet, "/", "")
	if got := rec.Header().Get("Location"); got != "/admin" {
		t.Errorf("Location = %q, want /admin", got)
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
		{name: "stylesheet", path: "/styles.css", wantContentType: "text/css"},
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

// Every asset the rendered page asks for has to exist, or the browser logs a
// 404. This walks the page as served rather than a file on disk, so a template
// that references a renamed asset fails here.
func TestRenderedPageReferencesOnlyExistingAssets(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	page := do(t, handler, http.MethodGet, "/admin", "")
	if page.Code != http.StatusOK {
		t.Fatalf("GET /admin = %d, want 200", page.Code)
	}

	refs := regexp.MustCompile(`(?:href|src)="(/[^"]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(refs) == 0 {
		t.Fatal("the page references no assets, the test would prove nothing")
	}

	for _, ref := range refs {
		path := ref[1]
		if rec := do(t, handler, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("the page references %s, which returns %d", path, rec.Code)
		}
	}
}
