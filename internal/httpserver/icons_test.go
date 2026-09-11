package httpserver

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// iconSource is where the favicon set is generated, by
// scripts/make-favicons.sh. The binary serves copies, because embedding reads
// from the package directory — and copies go stale silently, which is what this
// test is for. `make icons` refreshes them.
const iconSource = "../../images/favicons/logo-teamster-1"

func TestServedIconsMatchTheGeneratedSet(t *testing.T) {
	t.Parallel()

	served := map[string]string{
		"web/favicon.ico":                 "favicon.ico",
		"web/icons/favicon-16x16.png":     "favicon-16x16.png",
		"web/icons/favicon-32x32.png":     "favicon-32x32.png",
		"web/icons/favicon-48x48.png":     "favicon-48x48.png",
		"web/icons/apple-touch-icon.png":  "apple-touch-icon.png",
		"web/icons/icon-192-maskable.png": "icon-192-maskable.png",
		"web/icons/icon-512-maskable.png": "icon-512-maskable.png",
	}

	for path, source := range served {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			embedded, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read served icon: %v", err)
			}
			generated, err := os.ReadFile(filepath.Join(iconSource, source))
			if err != nil {
				t.Fatalf("read generated icon: %v", err)
			}

			if !bytes.Equal(embedded, generated) {
				t.Errorf("%s differs from %s; run `make icons`", path, filepath.Join(iconSource, source))
			}
		})
	}
}

// Every icon the pages and the manifest ask for has to be served, or a browser
// gets a 404 for something it was told to fetch.
func TestEveryReferencedIconIsServed(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	for _, path := range []string{
		"/favicon.ico",
		"/site.webmanifest",
		"/icons/favicon-16x16.png",
		"/icons/favicon-32x32.png",
		"/icons/favicon-48x48.png",
		"/icons/apple-touch-icon.png",
		"/icons/icon-192-maskable.png",
		"/icons/icon-512-maskable.png",
	} {
		rec := probe(t, handler, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s served nothing", path)
		}
	}
}
