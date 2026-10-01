package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

var (
	errDisableSelf  = errors.New("you cannot disable yourself")
	errDisableLocal = errors.New("the local login cannot be disabled: it is the way back in")
	errNoSuchUser   = errors.New("no user has signed in with that subject")
)

// usersPageSize is what the page lists before a search narrows it.
const usersPageSize = 200

func (s *Server) handleUsersPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	page := views.Users{
		Viewer: s.viewerFor(r),
		Search: query.Get("q"),
		Self:   principalSubject(r),
		Notice: query.Get("notice"),
		Error:  query.Get("error"),
	}
	users, err := s.store.ListUsers(ctx, page.Search, usersPageSize)
	if err != nil && page.Error == "" {
		page.Error = failureText(ctx, "list users", err)
	}
	page.Users = users

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.UsersPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render users page", err)
	}
}

// setUserDisabled refuses what would lock the caller, or everyone, out.
func (s *Server) setUserDisabled(r *http.Request, subject string, disabled bool) error {
	ctx := r.Context()
	if disabled && subject == principalSubject(r) {
		return userError{errDisableSelf}
	}
	user, err := s.store.GetUser(ctx, subject)
	if isNotFound(err) {
		return userError{errNoSuchUser}
	}
	if err != nil {
		return err
	}
	if user.Source == sourceLocal {
		return userError{errDisableLocal}
	}
	if disabled {
		return s.store.DisableUser(ctx, subject, principalSubject(r))
	}
	return s.store.EnableUser(ctx, subject)
}

func (s *Server) disableUserForm(r *http.Request) (string, error) {
	if err := s.setUserDisabled(r, r.PostFormValue("subject"), true); err != nil {
		return "", err
	}
	return "User disabled. Their sessions have ended.", nil
}

func (s *Server) enableUserForm(r *http.Request) (string, error) {
	if err := s.setUserDisabled(r, r.PostFormValue("subject"), false); err != nil {
		return "", err
	}
	return "User enabled.", nil
}

func (s *Server) handleUsersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	limit := usersPageSize
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}
	users, err := s.store.ListUsers(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	if users == nil {
		users = []models.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

// handleUserDisabledAPI is POST to disable and DELETE to enable.
func (s *Server) handleUserDisabledAPI(w http.ResponseWriter, r *http.Request) {
	var disabled bool
	switch r.Method {
	case http.MethodPost:
		disabled = true
	case http.MethodDelete:
	default:
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Subject == "" {
		writeJSONError(w, http.StatusBadRequest, "a JSON body with the subject is required")
		return
	}
	err := s.setUserDisabled(r, body.Subject, disabled)
	switch {
	case errors.Is(err, errNoSuchUser):
		writeError(w, r, http.StatusNotFound, err)
	case errors.Is(err, errDisableSelf), errors.Is(err, errDisableLocal):
		writeError(w, r, http.StatusConflict, err)
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, err)
	default:
		user, err := s.store.GetUser(r.Context(), body.Subject)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, user)
	}
}
