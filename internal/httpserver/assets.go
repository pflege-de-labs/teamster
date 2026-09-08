package httpserver

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
)

//go:embed web/*
var embeddedWeb embed.FS

// Go's mime table has no .webmanifest entry, so it would be served as plain
// text and browsers would ignore the manifest.
func registerMIMETypes() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
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
