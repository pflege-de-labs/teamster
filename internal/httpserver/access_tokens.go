package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

const maxTokenNameLength = 100

var (
	errTokenName      = fmt.Errorf("a token needs a name of at most %d characters", maxTokenNameLength)
	errTokenNameTaken = errors.New("a token with that name already exists")
	errTokenWebhooks  = errors.New("choose the webhooks the token may send to, from those you may use")
	errTokenNotYours  = errors.New("only its creator, a webhook admin or an admin may revoke a token")
)

// tokenWebhooks are what a token's scope may name.
var tokenWebhooks = []string{authz.WebhookAlertmanager, authz.WebhookUniversal}

// usableWebhooks are the webhooks this request's principal may use, which is
// what a token it mints may be scoped to.
func (s *Server) usableWebhooks(r *http.Request) []string {
	markChecked(r)
	var out []string
	for _, webhook := range tokenWebhooks {
		if s.allow(r, authz.ActionUse, authz.WebhookResource(webhook)) {
			out = append(out, webhook)
		}
	}
	return out
}

// manageTokens is whether the request may see and revoke everyone's tokens.
func (s *Server) manageTokens(r *http.Request) bool {
	return s.allow(r, authz.ActionAdminister, authz.WebhookResource(authz.WebhooksAll))
}

// visibleTokens are everyone's for a webhook admin, and the caller's own otherwise.
func (s *Server) visibleTokens(r *http.Request, tokens []models.AccessToken) []models.AccessToken {
	markChecked(r)
	if s.manageTokens(r) {
		return tokens
	}
	subject := principalSubject(r)
	out := []models.AccessToken{}
	for _, t := range tokens {
		if t.CreatedBy == subject {
			out = append(out, t)
		}
	}
	return out
}

// revokeToken deletes a token the caller made, or any for a webhook admin.
func (s *Server) revokeToken(r *http.Request, id string) error {
	ctx := r.Context()
	tokens, err := s.store.ListAccessTokens(ctx)
	if err != nil {
		return err
	}
	markChecked(r)
	for _, t := range tokens {
		if t.ID != id {
			continue
		}
		if t.CreatedBy != principalSubject(r) && !s.manageTokens(r) {
			return userError{errTokenNotYours}
		}
		return s.store.DeleteAccessToken(ctx, id)
	}
	return store.ErrNotFound
}

// accessTokenResponse carries the token only in the answer that created it.
type accessTokenResponse struct {
	models.AccessToken
	Token string `json:"token,omitempty"`
}

// issueAccessToken mints a token scoped to webhooks the caller may use,
// stores its digest and returns it in the clear (ADR 0077).
func (s *Server) issueAccessToken(r *http.Request, name string, scope []string) (models.AccessToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxTokenNameLength {
		return models.AccessToken{}, "", errTokenName
	}
	usable := s.usableWebhooks(r)
	slices.Sort(scope)
	scope = slices.Compact(scope)
	if len(scope) == 0 {
		return models.AccessToken{}, "", errTokenWebhooks
	}
	for _, webhook := range scope {
		if !slices.Contains(usable, webhook) {
			return models.AccessToken{}, "", errTokenWebhooks
		}
	}
	token, err := newAccessToken()
	if err != nil {
		return models.AccessToken{}, "", err
	}
	created, err := s.store.CreateAccessToken(r.Context(), models.AccessToken{
		Name:      name,
		TokenHash: hashToken(token),
		CreatedBy: principalSubject(r),
		Scope:     scope,
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
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, s.visibleTokens(r, items))
	case http.MethodPost:
		var body struct {
			Name  string   `json:"name"`
			Scope []string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			markChecked(r)
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		created, token, err := s.issueAccessToken(r, body.Name, body.Scope)
		switch {
		case errors.Is(err, errTokenWebhooks):
			writeError(w, r, http.StatusForbidden, err)
		case errors.Is(err, errTokenName):
			writeError(w, r, http.StatusBadRequest, err)
		case errors.Is(err, errTokenNameTaken):
			writeError(w, r, http.StatusConflict, err)
		case err != nil:
			writeError(w, r, http.StatusInternalServerError, err)
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
	if err := s.revokeToken(r, id); err != nil {
		writeRecordOrTokenError(w, r, err)
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
	created, token, err := s.issueAccessToken(r, r.PostFormValue("name"), r.PostForm["scope"])
	if err != nil {
		redirectTo("/admin/tokens", w, r, "", visibleError(r.Context(), "issue access token", err))
		return
	}
	s.renderTokens(w, r, views.Tokens{Notice: "Access token created.", NewToken: token, NewTokenName: created.Name})
}

func (s *Server) revokeAccessTokenForm(r *http.Request) (string, error) {
	if err := s.revokeToken(r, r.PostFormValue("id")); err != nil {
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
		page.Error = failureText(ctx, "list access tokens", err)
	}
	page.Tokens = s.visibleTokens(r, tokens)
	page.Usable = s.usableWebhooks(r)
	page.Manage = s.manageTokens(r)
	page.Self = principalSubject(r)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TokensPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render tokens page", err)
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

func writeRecordOrTokenError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errTokenNotYours):
		writeError(w, r, http.StatusForbidden, err)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, err)
	default:
		writeError(w, r, http.StatusInternalServerError, err)
	}
}
