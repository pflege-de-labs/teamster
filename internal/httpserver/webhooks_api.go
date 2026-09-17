package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// webhookEndpointResponse is an endpoint as the API returns it. Creating one
// answers with the token as well, because that is the only moment it exists in
// the clear; every later read leaves the field empty.
type webhookEndpointResponse struct {
	models.WebhookEndpoint
	Token string `json:"token,omitempty"`
	URL   string `json:"url,omitempty"`
}

func (s *Server) handleWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListWebhookEndpoints(ctx)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		visible, err := s.visibleWebhookEndpoints(r, items)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, visible)
	case http.MethodPost:
		endpoint, ok := s.decodeWebhookEndpoint(w, r, "")
		if !ok {
			return
		}

		token, err := newWebhookToken()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		endpoint.TokenHash = hashToken(token)

		created, err := s.store.CreateWebhookEndpoint(ctx, endpoint)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, webhookEndpointResponse{
			WebhookEndpoint: created,
			Token:           token,
			URL:             webhookURL(r, created, token),
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebhookEndpointByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	rotate := strings.HasSuffix(id, "/rotate")
	if rotate {
		id = strings.TrimSuffix(id, "/rotate")
	}
	if id == "" || strings.Contains(id, "/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if rotate {
		s.rotateWebhookEndpointToken(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetWebhookEndpoint(ctx, id)
		if err != nil {
			writeJSONError(w, notFoundOrInternal(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, webhookEndpointResponse{WebhookEndpoint: item})
	case http.MethodPut:
		endpoint, ok := s.decodeWebhookEndpoint(w, r, id)
		if !ok {
			return
		}
		updated, err := s.store.UpdateWebhookEndpoint(ctx, endpoint)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, webhookEndpointResponse{WebhookEndpoint: updated})
	case http.MethodDelete:
		if _, ok := s.authorizedEndpoint(w, r, id); !ok {
			return
		}
		if err := s.store.DeleteWebhookEndpoint(ctx, id); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// rotateWebhookEndpointToken is a POST rather than a PUT: it does not replace
// the endpoint, it mints a secret, and repeating it does not leave the same
// state behind.
func (s *Server) rotateWebhookEndpointToken(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	endpoint, ok := s.authorizedEndpoint(w, r, id)
	if !ok {
		return
	}

	token, err := newWebhookToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.RotateWebhookEndpointToken(r.Context(), id, hashToken(token)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, webhookEndpointResponse{
		WebhookEndpoint: endpoint,
		Token:           token,
		URL:             webhookURL(r, endpoint, token),
	})
}

// decodeWebhookEndpoint reads and vets the body of a write. It answers the
// request itself on every refusal, so a caller only has to stop.
func (s *Server) decodeWebhookEndpoint(w http.ResponseWriter, r *http.Request, id string) (models.WebhookEndpoint, bool) {
	var endpoint models.WebhookEndpoint
	if err := json.NewDecoder(r.Body).Decode(&endpoint); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return models.WebhookEndpoint{}, false
	}
	endpoint.ID = id
	endpoint.TeamSlug = strings.ToLower(strings.TrimSpace(endpoint.TeamSlug))
	endpoint.ChannelSlug = strings.ToLower(strings.TrimSpace(endpoint.ChannelSlug))

	if err := validateWebhookEndpoint(endpoint); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return models.WebhookEndpoint{}, false
	}

	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return models.WebhookEndpoint{}, false
	}
	if !allowed {
		writeJSONError(w, http.StatusForbidden, errDeliveryRefused.Error())
		return models.WebhookEndpoint{}, false
	}
	return endpoint, true
}

// authorizedEndpoint loads an endpoint and checks that this session may reach
// the channel behind it, for the writes that name no destination of their own.
func (s *Server) authorizedEndpoint(w http.ResponseWriter, r *http.Request, id string) (models.WebhookEndpoint, bool) {
	endpoint, err := s.store.GetWebhookEndpoint(r.Context(), id)
	if err != nil {
		writeJSONError(w, notFoundOrInternal(err), err.Error())
		return models.WebhookEndpoint{}, false
	}

	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return models.WebhookEndpoint{}, false
	}
	if !allowed {
		writeJSONError(w, http.StatusForbidden, errDeliveryRefused.Error())
		return models.WebhookEndpoint{}, false
	}
	return endpoint, true
}

func notFoundOrInternal(err error) int {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
