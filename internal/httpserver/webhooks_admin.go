package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errInvalidSlug says what a URL segment may be, because "invalid" leaves
// somebody guessing which of the two fields was wrong and how.
var errInvalidSlug = errors.New("a URL segment may hold lower case letters, digits and hyphens, and must start and end with a letter or digit")

// storeWebhook creates or updates the posted endpoint; token is set only for a new one, which needs it shown.
func (s *Server) storeWebhook(r *http.Request) (models.WebhookEndpoint, string, error) {
	posted := postedWebhook(r)
	if err := s.mayRecord(r, writeAction(posted.ID), typeWebhookEndpoint, posted.ID); err != nil {
		return posted, "", err
	}
	if err := validateWebhookEndpoint(posted); err != nil {
		return posted, "", userError{err}
	}
	allowed, err := s.mayDeliverToDestination(r, posted.DestinationID)
	if err != nil {
		return posted, "", err
	}
	if !allowed {
		return posted, "", userError{errDeliveryRefused}
	}
	if !s.mayAttachTemplate(r, posted.TemplateID) {
		return posted, "", userError{errTemplateRefused}
	}
	if err := s.endpointTemplateExists(r.Context(), posted); err != nil {
		if errors.Is(err, errUnknownTemplate) {
			err = userError{err}
		}
		return posted, "", err
	}

	// An existing endpoint keeps its token, so its senders keep working.
	if posted.ID != "" {
		_, err := s.updateEndpoint(r, posted)
		return posted, "", err
	}
	token, err := newWebhookToken()
	if err != nil {
		return posted, "", err
	}
	posted.TokenHash = hashToken(token)
	created, err := s.createEndpoint(r, posted)
	return created, token, err
}

// handleWebhookRotate replaces the token. It is a separate endpoint because
// saving an endpoint must not change the secret a sender is already using.
func (s *Server) handleWebhookRotate(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin/webhooks", w, r) {
		return
	}
	// The answer is the record page, so whoever may not read the record learns nothing, not even that it exists.
	endpoint, ok := s.readableEndpoint(w, r, r.PostFormValue("id"))
	if !ok {
		return
	}
	ctx := r.Context()
	back := recordEditPath("webhooks", endpoint.ID)
	if err := s.mayRecord(r, authz.ActionUpdate, typeWebhookEndpoint, endpoint.ID); err != nil {
		redirectTo(back, w, r, "", visibleError(ctx, "save webhook", err))
		return
	}

	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		redirectTo(back, w, r, "", visibleError(r.Context(), "save webhook", err))
		return
	}
	if !allowed {
		redirectTo(back, w, r, "", errDeliveryRefused.Error())
		return
	}

	token, err := newWebhookToken()
	if err != nil {
		redirectTo(back, w, r, "", visibleError(r.Context(), "save webhook", err))
		return
	}
	if err := s.store.RotateWebhookEndpointToken(ctx, endpoint.ID, hashToken(token)); err != nil {
		redirectTo(back, w, r, "", visibleError(r.Context(), "save webhook", err))
		return
	}

	s.revealWebhookURL(w, r, endpoint, token, "Webhook token replaced. The previous URL has stopped working.")
}

func (s *Server) deleteWebhookEndpoint(r *http.Request) (string, error) {
	ctx := r.Context()
	if err := s.mayRecord(r, authz.ActionDelete, typeWebhookEndpoint, r.PostFormValue("id")); err != nil {
		return "", err
	}
	endpoint, err := s.store.GetWebhookEndpoint(ctx, r.PostFormValue("id"))
	if err != nil {
		return "", err
	}
	// Out of scope reads as missing, as on the record page.
	if visible, err := s.listEndpoints(r, []models.WebhookEndpoint{endpoint}); err != nil {
		return "", err
	} else if len(visible) == 0 {
		return "", store.ErrNotFound
	}
	allowed, err := s.mayDeliverToDestination(r, endpoint.DestinationID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errDeliveryRefused
	}
	if err := s.deleteEndpoint(r, endpoint.ID); err != nil {
		return "", err
	}
	return "Webhook deleted.", nil
}

// postedWebhook is the endpoint as the form submitted it, slugs normalised.
func postedWebhook(r *http.Request) models.WebhookEndpoint {
	return models.WebhookEndpoint{
		ID:            r.PostFormValue("id"),
		TeamSlug:      strings.ToLower(strings.TrimSpace(r.PostFormValue("team_slug"))),
		ChannelSlug:   strings.ToLower(strings.TrimSpace(r.PostFormValue("channel_slug"))),
		DestinationID: r.PostFormValue("destination_id"),
		TemplateID:    r.PostFormValue("template_id"),
	}
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

// errUnknownTemplate is an endpoint naming a template that is not there, or
// one that does not handle Teams V2 payloads (ADR 0053).
var errUnknownTemplate = errors.New("the webhook names a template that does not exist or does not handle Teams V2 payloads")

func (s *Server) endpointTemplateExists(ctx context.Context, endpoint models.WebhookEndpoint) error {
	if endpoint.TemplateID == "" {
		return nil
	}
	template, err := s.store.GetTemplate(ctx, endpoint.TemplateID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errUnknownTemplate
		}
		return err
	}
	if !template.Handles(models.SourceTeamsV2) {
		return errUnknownTemplate
	}
	return nil
}

func (s *Server) webhookURL(r *http.Request, endpoint models.WebhookEndpoint, token string) string {
	return fmt.Sprintf("%s/teamsv2/%s/%s/%s", s.webhookBaseURL(r), endpoint.TeamSlug, endpoint.ChannelSlug, token)
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
