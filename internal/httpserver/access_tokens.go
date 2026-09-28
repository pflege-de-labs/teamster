package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

const maxTokenNameLength = 100

var (
	errTokenName      = fmt.Errorf("a token needs a name of at most %d characters", maxTokenNameLength)
	errTokenNameTaken = errors.New("a token with that name already exists")
)

// accessTokenResponse carries the token only in the answer that created it.
type accessTokenResponse struct {
	models.AccessToken
	Token string `json:"token,omitempty"`
}

// issueAccessToken mints a token, stores its digest and returns it in the clear.
func (s *Server) issueAccessToken(r *http.Request, name string) (models.AccessToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxTokenNameLength {
		return models.AccessToken{}, "", errTokenName
	}
	token, err := newAccessToken()
	if err != nil {
		return models.AccessToken{}, "", err
	}
	created, err := s.store.CreateAccessToken(r.Context(), models.AccessToken{
		Name:      name,
		TokenHash: hashToken(token),
		CreatedBy: principalSubject(r),
	})
	if errors.Is(err, store.ErrConflict) {
		return models.AccessToken{}, "", errTokenNameTaken
	}
	if err != nil {
		return models.AccessToken{}, "", err
	}
	return created, token, nil
}

func (s *Server) handleAccessTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListAccessTokens(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if items == nil {
			items = []models.AccessToken{}
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		created, token, err := s.issueAccessToken(r, body.Name)
		switch {
		case errors.Is(err, errTokenName):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errTokenNameTaken):
			writeJSONError(w, http.StatusConflict, err.Error())
		case err != nil:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		default:
			writeJSON(w, http.StatusCreated, accessTokenResponse{AccessToken: created, Token: token})
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAccessTokenByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tokens/")
	if id == "" || strings.Contains(id, "/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.store.DeleteAccessToken(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) handleTokensPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.renderTokens(w, r, views.Tokens{Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error")})
}

// handleTokenForm answers by rendering rather than redirecting, for the reason
// handleWebhookForm gives: a redirect would put the token in a URL.
func (s *Server) handleTokenForm(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin/tokens", w, r) {
		return
	}
	created, token, err := s.issueAccessToken(r, r.PostFormValue("name"))
	if err != nil {
		redirectTo("/admin/tokens", w, r, "", err.Error())
		return
	}
	s.renderTokens(w, r, views.Tokens{Notice: "Access token created.", NewToken: token, NewTokenName: created.Name})
}

func (s *Server) revokeAccessTokenForm(r *http.Request) (string, error) {
	if err := s.store.DeleteAccessToken(r.Context(), r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Access token revoked. Senders using it are refused from now on.", nil
}

func (s *Server) renderTokens(w http.ResponseWriter, r *http.Request, page views.Tokens) {
	ctx := r.Context()
	page.Viewer = s.viewerFor(r)
	page.WebhookURL = webhookBaseURL(r) + "/webhook/alertmanager"
	page.ConfigTokenSet = s.cfg.Webhook.Token != ""

	tokens, err := s.store.ListAccessTokens(ctx)
	if err != nil && page.Error == "" {
		page.Error = err.Error()
	}
	page.Tokens = tokens

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TokensPage(page).Render(ctx, w); err != nil {
		logError("render tokens page", err)
	}
}

// webhookBaseURL is built from the request, like webhookURL, because the host
// the admin is reading is the host the sender will be pointed at.
func webhookBaseURL(r *http.Request) string {
	scheme := "http"
	if isTLS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
