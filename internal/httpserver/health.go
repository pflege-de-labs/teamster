package httpserver

import (
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
)

// Liveness and readiness are separate questions, and answering both with the
// same check is how a cluster ends up restarting a process that was only
// waiting on its database.
//
// Neither endpoint authenticates: a probe has no credentials, and the answer
// says nothing an attacker does not already learn by connecting to the port.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Liveness asks whether this process should be killed. It answers for the
	// process alone: a database that has gone away is not something a restart
	// fixes, and restarting on it turns an outage into a crash loop.
	writePlain(w, http.StatusOK, "ok")
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Shutting down is the first thing readiness reports, so a rolling update
	// takes this instance out of the load balancer before it stops accepting
	// the connections that are still being routed to it.
	if s.draining.Load() {
		writePlain(w, http.StatusServiceUnavailable, "shutting down")
		return
	}

	// The store is the one dependency a request cannot do without. Microsoft
	// Graph deliberately is not checked: it is somebody else's service, and
	// taking this instance out of rotation when it is unreachable would stop
	// the admin UI from working precisely when an operator wants to look at it.
	if err := s.store.Ping(); err != nil {
		logError("readiness", err)
		writePlain(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}

	writePlain(w, http.StatusOK, "ok")
}

// writePlain keeps the body to a word. A probe reads the status code, and
// anything more detailed here is only useful to somebody who should not be
// reading it.
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}

// localized decides which language a page renders in, once per request, and
// carries it in the context — which is what the templ components read. It wraps
// everything rather than only the pages: a handler that renders nothing simply
// never asks.
func (s *Server) localized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chosen, fromCookie := s.languagePreference(r)
		tag := i18n.Parse(chosen)

		ctx := i18n.WithLanguage(r.Context(), s.text, tag)
		ctx = views.WithLanguageChoice(ctx, views.LanguageChoice{
			Languages: s.languageTags(),
			Current:   tag.String(),
			Chosen:    fromCookie,
			Return:    r.URL.RequestURI(),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
