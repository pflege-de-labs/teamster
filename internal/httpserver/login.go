package httpserver

import (
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/authz"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
)

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.currentSession(r); ok {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := views.Login{
			Error:      r.URL.Query().Get("error"),
			OIDC:       s.oidcEnabled(),
			LocalLogin: localLoginConfigured(s.cfg),
		}
		if err := views.LoginPage(page).Render(r.Context(), w); err != nil {
			logError("render login page", err)
		}
	case http.MethodPost:
		s.handleLocalLogin(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// The local credentials are the way back in when the identity provider is
// unreachable or the claim is misconfigured.
func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin login rejected", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		loginFailed(w, r, "invalid form submission")
		return
	}

	user := r.PostFormValue("username")
	password := r.PostFormValue("password")

	// Both comparisons always run, so a wrong username and a wrong password
	// cost the same time.
	userOK := safeEquals(user, s.cfg.Admin.Username)
	passwordOK := safeEquals(password, s.cfg.Admin.Password)
	if !localLoginConfigured(s.cfg) || !userOK || !passwordOK {
		loginFailed(w, r, "wrong username or password")
		return
	}

	// The local credentials are the way back in when the provider is wrong or
	// unreachable, so they administer.
	if err := s.startSession(w, r, user, user, "local", []authz.Role{authz.RoleAdmin}); err != nil {
		logError("start session", err)
		loginFailed(w, r, "could not start a session")
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin logout rejected", http.StatusForbidden)
		return
	}

	s.endSession(w, r)
	http.Redirect(w, r, "/admin/login?error=signed+out", http.StatusSeeOther)
}
