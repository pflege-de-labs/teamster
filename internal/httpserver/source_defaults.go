package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errBadSourceDefault is a source default choice the store refuses because of
// what was asked, not because it failed.
var errBadSourceDefault = errors.New("invalid source default")

// sourceDefaultTemplates is every source's default template over the API
// (ADR 0055). A source missing from the map, or mapped to "", has the
// built-in message.
type sourceDefaultTemplates struct {
	Templates map[string]string `json:"templates"`
}

func (s *Server) handleSourceDefaults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		defaults, err := s.store.SourceDefaultTemplates(ctx)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, sourceDefaultTemplates{Templates: defaults})
	case http.MethodPut:
		var body sourceDefaultTemplates
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.setSourceDefaults(r, body.Templates); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errBadSourceDefault) {
				status = http.StatusBadRequest
			}
			writeError(w, r, status, err)
			return
		}
		defaults, err := s.store.SourceDefaultTemplates(ctx)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, sourceDefaultTemplates{Templates: defaults})
	default:
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// saveSourceDefaults is the templates panel's form: one select per source,
// named template_<source>, all saved together.
func (s *Server) saveSourceDefaults(r *http.Request) (string, error) {
	choices := map[string]string{}
	for _, source := range models.AllSources() {
		choices[source] = r.PostFormValue("template_" + source)
	}
	if err := s.setSourceDefaults(r, choices); err != nil {
		return "", err
	}
	return "Default templates changed.", nil
}

// setSourceDefaults writes the sources choices names in one transaction, so a
// refused choice leaves the others as they were. A source it does not name is
// left alone.
func (s *Server) setSourceDefaults(r *http.Request, choices map[string]string) error {
	for source := range choices {
		if _, err := models.NormalizeSources([]string{source}); err != nil {
			return fmt.Errorf("%w: %w", errBadSourceDefault, err)
		}
	}
	err := s.store.WithTx(r.Context(), func(ctx context.Context, tx store.Store) error {
		for _, source := range models.AllSources() {
			id, named := choices[source]
			if !named {
				continue
			}
			if err := tx.SetSourceDefaultTemplate(ctx, source, id); err != nil {
				return err
			}
		}
		return nil
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("%w: that template does not exist", errBadSourceDefault)
	case errors.Is(err, store.ErrTemplateSource):
		return fmt.Errorf("%w: %w", errBadSourceDefault, err)
	}
	return err
}
