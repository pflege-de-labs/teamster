package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListTemplates()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var t models.Template
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := templates.Validate(t); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		created, err := s.store.CreateTemplate(t)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTemplateByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/templates/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetTemplate(id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeJSONError(w, status, err.Error())
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
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := s.store.UpdateTemplate(t)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteTemplate(id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDestinations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListDestinations()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		visible, err := s.visibleDestinations(r, items)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, visible)
	case http.MethodPost:
		var d models.Destination
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if allowed, err := s.mayDeliverTo(r, d.TeamID, d.ChannelID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		} else if !allowed {
			writeJSONError(w, http.StatusForbidden, errDeliveryRefused.Error())
			return
		}
		created, err := s.store.CreateDestination(d)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDestinationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/destinations/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetDestination(id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeJSONError(w, status, err.Error())
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
		if allowed, err := s.mayDeliverTo(r, d.TeamID, d.ChannelID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		} else if !allowed {
			writeJSONError(w, http.StatusForbidden, errDeliveryRefused.Error())
			return
		}
		updated, err := s.store.UpdateDestination(d)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteDestination(id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListRoutes()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var rt models.Route
		if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.validateRoute(rt); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if allowed, err := s.mayDeliverToDestination(r, rt.DestinationID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		} else if !allowed {
			writeJSONError(w, http.StatusForbidden, errDeliveryRefused.Error())
			return
		}
		created, err := s.store.CreateRoute(rt)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRouteByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/routes/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetRoute(id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeJSONError(w, status, err.Error())
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
		if err := s.validateRoute(rt); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if allowed, err := s.mayDeliverToDestination(r, rt.DestinationID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		} else if !allowed {
			writeJSONError(w, http.StatusForbidden, errDeliveryRefused.Error())
			return
		}
		updated, err := s.store.UpdateRoute(rt)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.validateRouteDelete(id); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.store.DeleteRoute(id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
