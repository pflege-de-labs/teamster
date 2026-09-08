package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
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
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var d models.Destination
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
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
		updated, err := s.store.UpdateRoute(rt)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteRoute(id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
