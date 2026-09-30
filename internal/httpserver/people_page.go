package httpserver

import (
	"errors"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// maxProblems bounds the failures the people page lists.
const maxProblems = 50

// errRunActive refuses a second run while one is requested or running.
var errRunActive = errors.New("an install run is already requested or running")

// handlePeoplePage shows how far installing the app for everyone got, and
// offers the button that starts a run (ADR 0059).
func (s *Server) handlePeoplePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	page := views.People{
		Viewer: s.viewerFor(r),
		Notice: r.URL.Query().Get("notice"),
		Error:  r.URL.Query().Get("error"),
	}
	fail := func(err error) {
		if page.Error == "" {
			page.Error = failureText(ctx, "load people page", err)
		}
	}

	counts, err := s.store.CountDirectoryUsersByState(ctx)
	if err != nil {
		fail(err)
	}
	for _, state := range installStateOrder {
		page.Counts = append(page.Counts, views.StateCount{State: string(state), Users: counts[state]})
	}

	run, err := s.store.LatestDirectoryRun(ctx)
	switch {
	case err == nil:
		page.Run = &run
	case !errors.Is(err, store.ErrNotFound):
		fail(err)
	}

	if page.Problems, err = s.store.ListDirectoryUserProblems(ctx, maxProblems); err != nil {
		fail(err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.PeoplePage(page).Render(ctx, w); err != nil {
		logError(ctx, "render people page", err)
	}
}

// installStateOrder is the order the page counts states in.
var installStateOrder = []models.InstallState{
	models.InstallInstalled, models.InstallUnknown, models.InstallRemoved,
	models.InstallFailed, models.InstallIneligible, models.InstallDeparted,
}

// requestInstallRun is the button: a manual run the next reconciler tick
// claims, on whichever replica gets there first.
func (s *Server) requestInstallRun(r *http.Request) (string, error) {
	if _, err := s.requestRun(r); err != nil {
		return "", err
	}
	return i18n.T(r.Context(), "people.requested"), nil
}

func (s *Server) requestRun(r *http.Request) (models.DirectoryRun, error) {
	run, err := s.store.RequestDirectoryRun(r.Context(), models.DirectoryRun{
		Kind: models.RunManual, RequestedBy: principalSubject(r),
	})
	if errors.Is(err, store.ErrConflict) {
		return models.DirectoryRun{}, userError{errRunActive}
	}
	return run, err
}

// handlePeopleRuns answers GET /api/people/runs/latest with the latest run
// and the counts by state.
func (s *Server) handlePeopleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()
	counts, err := s.store.CountDirectoryUsersByState(ctx)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	body := struct {
		Run    *models.DirectoryRun          `json:"run"`
		Counts map[models.InstallState]int64 `json:"counts"`
	}{Counts: counts}
	run, err := s.store.LatestDirectoryRun(ctx)
	switch {
	case err == nil:
		body.Run = &run
	case !errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// handlePeopleInstall answers POST /api/people/install: 202 with the requested
// run, or 409 while one is active.
func (s *Server) handlePeopleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	run, err := s.requestRun(r)
	switch {
	case errors.Is(err, errRunActive):
		writeError(w, r, http.StatusConflict, err)
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusAccepted, run)
	}
}
