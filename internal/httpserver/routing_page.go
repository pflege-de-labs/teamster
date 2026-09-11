package httpserver

import (
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
)

func (s *Server) handleRoutingPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.RoutingPage(s.viewerFor(r)).Render(r.Context(), w); err != nil {
		logError("render routing page", err)
	}
}
