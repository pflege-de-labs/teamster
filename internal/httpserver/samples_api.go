package httpserver

import (
	"net/http"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// maxListedSamples bounds one answer however many keys have been seen: the
// editor holds all of it in memory for the life of the page.
const maxListedSamples = 10000

// samplesResponse is what the editor completes from. Label values are most
// recently seen first; label and annotation keys are sorted by name.
type samplesResponse struct {
	Labels      map[string][]string `json:"labels"`
	Annotations []string            `json:"annotations"`
}

// handleSamples answers GET /api/samples. authorize has already required edit
// on templates or routes, see alternativeAuthorization.
func (s *Server) handleSamples(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	resp := samplesResponse{Labels: map[string][]string{}, Annotations: []string{}}
	if !s.cfg.Samples.Enabled {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rows, err := s.store.ListAlertSamples(r.Context(), maxListedSamples)
	if err != nil {
		logError("list alert samples", err)
		writeJSONError(w, http.StatusInternalServerError, "samples could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, groupSamples(rows, s.cfg.Samples.MaxValuesPerKey))
}

// groupSamples relies on the store's order: grouped by key, newest first.
// Pruning runs hourly, so the cap is applied here too.
func groupSamples(rows []models.AlertSample, maxValues int) samplesResponse {
	resp := samplesResponse{Labels: map[string][]string{}, Annotations: []string{}}
	for _, row := range rows {
		switch row.Kind {
		case models.SampleLabel:
			if values := resp.Labels[row.Key]; len(values) < maxValues {
				resp.Labels[row.Key] = append(values, row.Value)
			}
		case models.SampleAnnotation:
			resp.Annotations = append(resp.Annotations, row.Key)
		}
	}
	sort.Strings(resp.Annotations)
	return resp
}

// mayComplete is whether the viewer edits something the samples complete.
func (s *Server) mayComplete(r *http.Request) bool {
	subject, roles := principalOf(r)
	return s.authz.Allow(subject, roles, authz.ActionEdit, authz.Resource{Type: "Template"}) ||
		s.authz.Allow(subject, roles, authz.ActionEdit, authz.Resource{Type: "Route"})
}
