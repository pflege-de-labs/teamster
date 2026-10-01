package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListTemplates(ctx)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, readable(s, r, typeTemplate, items, templateID))
	case http.MethodPost:
		if err := s.mayRecord(r, authz.ActionCreate, typeTemplate, ""); err != nil {
			writeRecordError(w, r, err)
			return
		}
		var t models.Template
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := templates.Validate(t); err != nil {
			writeError(w, r, http.StatusBadRequest, err)
			return
		}
		created, err := s.createTemplate(r, t)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTemplateByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimPrefix(r.URL.Path, "/api/templates/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := s.mayRecord(r, recordAction(r.Method), typeTemplate, id); err != nil {
		writeRecordError(w, r, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetTemplate(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, r, status, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var t models.Template
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		t.ID = id
		if err := templates.Validate(t); err != nil {
			writeError(w, r, http.StatusBadRequest, err)
			return
		}
		updated, err := s.store.UpdateTemplate(ctx, t)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.deleteOwned(ctx, typeTemplate, id, func(ctx context.Context, tx store.Store) error {
			return tx.DeleteTemplate(ctx, id)
		}); err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDestinations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListDestinations(ctx)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		visible, err := s.listDestinations(r, items)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, visible)
	case http.MethodPost:
		if err := s.mayRecord(r, authz.ActionCreate, typeDestination, ""); err != nil {
			writeRecordError(w, r, err)
			return
		}
		var d models.Destination
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if allowed, err := s.mayDeliverTo(r, d.TeamID, d.ChannelID); err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		} else if !allowed {
			writeError(w, r, http.StatusForbidden, errDeliveryRefused)
			return
		}
		created, err := s.createDestination(r, d)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDestinationByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimPrefix(r.URL.Path, "/api/destinations/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if base, ok := strings.CutSuffix(id, "/default"); ok {
		s.handleDefaultDestination(w, r, base)
		return
	}
	if err := s.mayRecord(r, recordAction(r.Method), typeDestination, id); err != nil {
		writeRecordError(w, r, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetDestination(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, r, status, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var d models.Destination
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		d.ID = id
		if allowed, err := s.mayRepointDestination(r, d); err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		} else if !allowed {
			writeError(w, r, http.StatusForbidden, errDeliveryRefused)
			return
		}
		updated, err := s.store.UpdateDestination(ctx, d)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.deleteOwned(ctx, typeDestination, id, func(ctx context.Context, tx store.Store) error {
			return tx.DeleteDestination(ctx, id)
		}); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrDefaultDestination) {
				status = http.StatusConflict
			}
			writeError(w, r, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleDefaultDestination makes one destination the global default.
func (s *Server) handleDefaultDestination(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.store.SetDefaultDestination(r.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, r, status, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "default"})
}

// writeRouteError keeps the distinction the separate validate call used to
// make for free: a route the caller can fix is a 400, anything else is a 500.
func writeRouteError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid invalidRoute
	if errors.As(err, &invalid) {
		writeError(w, r, http.StatusBadRequest, invalid)
		return
	}
	writeError(w, r, http.StatusInternalServerError, err)
}

// writeRouteRefusal answers a route write whose targets were refused with a
// 403, and a failure to check them with a 500.
func writeRouteRefusal(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errDeliveryRefused) || errors.Is(err, errRecipientRefused) || errors.Is(err, errTemplateRefused) {
		writeError(w, r, http.StatusForbidden, err)
		return
	}
	writeError(w, r, http.StatusInternalServerError, err)
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListRoutes(ctx)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, readable(s, r, typeRoute, items, routeID))
	case http.MethodPost:
		if err := s.mayRecord(r, authz.ActionCreate, typeRoute, ""); err != nil {
			writeRecordError(w, r, err)
			return
		}
		var rt models.Route
		if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.routeWriteRefusal(r, rt); err != nil {
			writeRouteRefusal(w, r, err)
			return
		}
		created, err := s.saveRouteChecked(r, rt)
		if err != nil {
			writeRouteError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRouteByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimPrefix(r.URL.Path, "/api/routes/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := s.mayRecord(r, recordAction(r.Method), typeRoute, id); err != nil {
		writeRecordError(w, r, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetRoute(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, r, status, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var rt models.Route
		if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		rt.ID = id
		if err := s.routeWriteRefusal(r, rt); err != nil {
			writeRouteRefusal(w, r, err)
			return
		}
		updated, err := s.saveRouteChecked(r, rt)
		if err != nil {
			writeRouteError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.routeDeleteRefusal(r, id); err != nil {
			writeRouteRefusal(w, r, err)
			return
		}
		if err := s.deleteRouteChecked(ctx, id); err != nil {
			writeRouteError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
