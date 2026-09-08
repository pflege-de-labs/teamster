package httpserver

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var embeddedWeb embed.FS

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	fileServer := http.FileServer(http.FS(sub))
	if r.URL.Path == "/admin" || r.URL.Path == "/" {
		r.URL.Path = "/index.html"
	}
	fileServer.ServeHTTP(w, r)
}
