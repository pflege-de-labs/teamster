package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/store"
)

// errUnknownCatchAllTemplate is a catch-all template choice naming nothing.
var errUnknownCatchAllTemplate = errors.New("that template does not exist")

// globalDefaultTemplate is the catch-all route's template over the API
// (ADR 0050). An empty id is the built-in default message.
type globalDefaultTemplate struct {
	TemplateID string `json:"template_id"`
}

func (s *Server) handleGlobalDefault(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		id, err := s.store.GetGlobalDefaultTemplate(ctx)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, globalDefaultTemplate{TemplateID: id})
	case http.MethodPut:
		var body globalDefaultTemplate
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.setGlobalDefaultTemplate(r, body.TemplateID); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errUnknownCatchAllTemplate) {
				status = http.StatusBadRequest
			}
			writeError(w, r, status, err)
			return
		}
		writeJSON(w, http.StatusOK, body)
	default:
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) saveGlobalDefault(r *http.Request) (string, error) {
	if err := s.setGlobalDefaultTemplate(r, r.PostFormValue("template_id")); err != nil {
		return "", err
	}
	return "Global default template changed.", nil
}

func (s *Server) setGlobalDefaultTemplate(r *http.Request, templateID string) error {
	err := s.store.SetGlobalDefaultTemplate(r.Context(), templateID)
	if errors.Is(err, store.ErrNotFound) {
		return errUnknownCatchAllTemplate
	}
	return err
}
