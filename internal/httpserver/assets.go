package httpserver

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
)

//go:embed web/*
var embeddedWeb embed.FS

// Both types are pinned because Go falls back to the host's mime table, which
// has no .webmanifest entry at all and disagrees about .ico between platforms.
func registerMIMETypes() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
}

// handleAssets serves the embedded static files. The admin page itself is
// rendered by handleAdminPage, so a bare "/" redirects there.
func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if r.URL.Path == "/" {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}

	http.FileServerFS(sub).ServeHTTP(w, r)
}
