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

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Serving index.html by name, rather than rewriting the path, avoids the
	// FileServer redirect from /index.html back to ./ and the resulting loop.
	if r.URL.Path == "/admin" || r.URL.Path == "/" {
		http.ServeFileFS(w, r, sub, "index.html")
		return
	}

	http.FileServerFS(sub).ServeHTTP(w, r)
}
