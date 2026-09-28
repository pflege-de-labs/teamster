package httpserver

import (
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
)

// handleUserInfoPage shows what the session knows about who holds it. It sits
// outside authorize: a user with no role is who most needs to read it.
func (s *Server) handleUserInfoPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	session, ok := s.currentSession(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}

	page := views.UserInfo{
		Viewer:    s.viewerFor(r),
		Subject:   session.Subject,
		Source:    session.Source,
		ExpiresAt: session.ExpiresAt,
		Identity:  session.Identity,
	}
	if err := views.UserInfoPage(page).Render(ctx, w); err != nil {
		logError("render user info page", err)
	}
}
