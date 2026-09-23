package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errInvalidSlug says what a URL segment may be, because "invalid" leaves
// somebody guessing which of the two fields was wrong and how.
var errInvalidSlug = errors.New("a URL segment may hold lower case letters, digits and hyphens, and must start and end with a letter or digit")

// handleWebhookForm saves an endpoint. Creating one answers by rendering the
// page rather than by redirecting, because the answer contains the token: a
// redirect would carry it in a query string, and from there into the browser
// history and this server's own access log.
func (s *Server) handleWebhookForm(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin", w, r) {
		return
	}

	endpoint, err := webhookEndpointFromForm(r)
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}

	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}
	if !allowed {
		redirectTo("/admin", w, r, "", errDeliveryRefused.Error())
		return
	}
	if err := s.endpointTemplateExists(r.Context(), endpoint); err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}

	// An existing endpoint keeps its token, so its senders keep working.
	if endpoint.ID != "" {
		if _, err := s.store.UpdateWebhookEndpoint(r.Context(), endpoint); err != nil {
			redirectTo("/admin", w, r, "", err.Error())
			return
		}
		redirectTo("/admin", w, r, "Webhook updated.", "")
		return
	}

	token, err := newWebhookToken()
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}
	endpoint.TokenHash = hashToken(token)

	created, err := s.store.CreateWebhookEndpoint(r.Context(), endpoint)
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}

	s.revealWebhookURL(w, r, created, token, "Webhook created.")
}

// handleWebhookRotate replaces the token. It is a separate endpoint because
// saving an endpoint must not change the secret a sender is already using.
func (s *Server) handleWebhookRotate(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin", w, r) {
		return
	}

	ctx := r.Context()
	endpoint, err := s.store.GetWebhookEndpoint(ctx, r.PostFormValue("id"))
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}

	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}
	if !allowed {
		redirectTo("/admin", w, r, "", errDeliveryRefused.Error())
		return
	}

	token, err := newWebhookToken()
	if err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}
	if err := s.store.RotateWebhookEndpointToken(ctx, endpoint.ID, hashToken(token)); err != nil {
		redirectTo("/admin", w, r, "", err.Error())
		return
	}

	s.revealWebhookURL(w, r, endpoint, token, "Webhook token replaced. The previous URL has stopped working.")
}

func (s *Server) deleteWebhookEndpoint(r *http.Request) (string, error) {
	ctx := r.Context()
	endpoint, err := s.store.GetWebhookEndpoint(ctx, r.PostFormValue("id"))
	if err != nil {
		return "", err
	}
	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errDeliveryRefused
	}
	if err := s.store.DeleteWebhookEndpoint(ctx, endpoint.ID); err != nil {
		return "", err
	}
	return "Webhook deleted.", nil
}

// revealWebhookURL renders the admin page with the one sight of the token there
// will ever be.
func (s *Server) revealWebhookURL(w http.ResponseWriter, r *http.Request, endpoint models.WebhookEndpoint, token, notice string) {
	page := s.adminPage(r, notice, "")
	page.NewWebhookURL = webhookURL(r, endpoint, token)
	s.renderAdmin(w, r, page)
}

func webhookEndpointFromForm(r *http.Request) (models.WebhookEndpoint, error) {
	endpoint := models.WebhookEndpoint{
		ID:            r.PostFormValue("id"),
		TeamSlug:      strings.ToLower(strings.TrimSpace(r.PostFormValue("team_slug"))),
		ChannelSlug:   strings.ToLower(strings.TrimSpace(r.PostFormValue("channel_slug"))),
		DestinationID: r.PostFormValue("destination_id"),
		TemplateID:    r.PostFormValue("template_id"),
	}
	if err := validateWebhookEndpoint(endpoint); err != nil {
		return models.WebhookEndpoint{}, err
	}
	return endpoint, nil
}

func validateWebhookEndpoint(endpoint models.WebhookEndpoint) error {
	if !slugPattern.MatchString(endpoint.TeamSlug) || !slugPattern.MatchString(endpoint.ChannelSlug) {
		return errInvalidSlug
	}
	if endpoint.DestinationID == "" {
		return errors.New("a webhook needs a destination to post into")
	}
	return nil
}

// errUnknownTemplate is an endpoint naming a template that is not there, which
// would fail every message it receives.
var errUnknownTemplate = errors.New("the webhook names a template that does not exist")

func (s *Server) endpointTemplateExists(ctx context.Context, endpoint models.WebhookEndpoint) error {
	if endpoint.TemplateID == "" {
		return nil
	}
	if _, err := s.store.GetTemplate(ctx, endpoint.TemplateID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errUnknownTemplate
		}
		return err
	}
	return nil
}

// webhookURL is built from the request rather than from configuration, because
// the host the admin is reading the page on is the host their sender will be
// pointed at.
func webhookURL(r *http.Request, endpoint models.WebhookEndpoint, token string) string {
	scheme := "http"
	if isTLS(r) {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/teamsv2/%s/%s/%s", scheme, r.Host, endpoint.TeamSlug, endpoint.ChannelSlug, token)
}

// visibleWebhookEndpoints hides the endpoints whose channel this session may not
// see, the way visibleDestinations does: an endpoint is a way into a channel, so
// listing one says the channel exists.
func (s *Server) visibleWebhookEndpoints(r *http.Request, endpoints []models.WebhookEndpoint) ([]models.WebhookEndpoint, error) {
	if len(endpoints) == 0 {
		return endpoints, nil
	}

	visible := make([]models.WebhookEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		allowed, err := s.mayViewWebhookEndpoint(r.Context(), r, endpoint)
		if err != nil {
			return nil, err
		}
		if allowed {
			visible = append(visible, endpoint)
		}
	}
	return visible, nil
}

func (s *Server) mayViewWebhookEndpoint(ctx context.Context, r *http.Request, endpoint models.WebhookEndpoint) (bool, error) {
	destination, err := s.store.GetDestination(ctx, endpoint.DestinationID)
	if err != nil {
		// An endpoint pointing at a destination that is gone is broken, and
		// showing it is how somebody finds out.
		if errors.Is(err, store.ErrNotFound) {
			return true, nil
		}
		return false, err
	}

	visible, err := s.visibleDestinations(r, []models.Destination{destination})
	if err != nil {
		return false, err
	}
	return len(visible) == 1, nil
}
