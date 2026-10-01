package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errRecipientRefused is what a write pointing a route at someone else's chat
// gets (ADR 0047).
var errRecipientRefused = errors.New("a route may deliver only to your own chat")

// errAddressedRefused is what a write touching a route that delivers to the
// people a message names gets without the right to (ADR 0062).
var errAddressedRefused = errors.New("only an admin may deliver to the people a message names")

// Route form target values: one select names the channel or the person, so the
// two cannot both be submitted.
const (
	targetDestination = "destination:"
	targetRecipient   = "recipient:"
	targetAddressed   = "addressed"
)

// parseRouteTarget reads the route form's target select. An empty value is a
// child inheriting its parent's target.
func parseRouteTarget(raw string) (destinationID, recipientID string, addressed bool, err error) {
	switch {
	case raw == "":
		return "", "", false, nil
	case raw == targetAddressed:
		return "", "", true, nil
	case strings.HasPrefix(raw, targetDestination):
		return strings.TrimPrefix(raw, targetDestination), "", false, nil
	case strings.HasPrefix(raw, targetRecipient):
		return "", strings.TrimPrefix(raw, targetRecipient), false, nil
	}
	return "", "", false, userError{fmt.Errorf("unknown route target %q", raw)}
}

// mayAddress answers whether this session may point a route at the people a
// message names.
func (s *Server) mayAddress(r *http.Request) bool {
	subject, roles := principalOf(r)
	return s.policies(r).Allow(subject, roles, authz.ActionDeliverToAddressed, authz.AddressedResource)
}

// mayTargetRecipient answers whether this session may point a route at a
// recipient's chat. An unknown recipient names nobody, so there is no one to
// protect; the routing graph shows it as broken.
func (s *Server) mayTargetRecipient(r *http.Request, recipientID string) (bool, error) {
	if recipientID == "" {
		return true, nil
	}
	recipient, err := s.store.GetRecipient(r.Context(), recipientID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	return s.mayTargetRecipientOf(r, recipient), nil
}

func (s *Server) mayTargetRecipientOf(r *http.Request, recipient models.Recipient) bool {
	subject, roles := principalOf(r)
	return s.policies(r).Allow(subject, roles, authz.ActionDeliverToRecipient, authz.RecipientResource(recipient.Subject))
}

// routeWriteRefusal checks every target a save touches: the channel and person
// it names, and the person the stored route delivers to, so an editor cannot
// repoint or rewrite someone else's personal route.
func (s *Server) routeWriteRefusal(r *http.Request, route models.Route) error {
	allowed, err := s.mayDeliverToDestination(r, route.DestinationID)
	if err != nil {
		return err
	}
	if !allowed {
		return errDeliveryRefused
	}
	if allowed, err = s.mayTargetRecipient(r, route.RecipientID); err != nil {
		return err
	} else if !allowed {
		return errRecipientRefused
	}
	if route.Addressed && !s.mayAddress(r) {
		return errAddressedRefused
	}
	if route.ID == "" {
		return nil
	}
	return s.routeDeleteRefusal(r, route.ID)
}

// routeDeleteRefusal refuses touching a stored route that delivers to someone
// else's chat. A route that does not exist is left for the store to report.
func (s *Server) routeDeleteRefusal(r *http.Request, id string) error {
	existing, err := s.store.GetRoute(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	allowed, err := s.mayTargetRecipient(r, existing.RecipientID)
	if err != nil {
		return err
	}
	if !allowed {
		return errRecipientRefused
	}
	if existing.Addressed && !s.mayAddress(r) {
		return errAddressedRefused
	}
	return nil
}

// targetableRecipients narrows the recipient list to the chats this session may
// route to.
func (s *Server) targetableRecipients(r *http.Request, recipients []models.Recipient) []models.Recipient {
	out := make([]models.Recipient, 0, len(recipients))
	for _, recipient := range recipients {
		if s.mayTargetRecipientOf(r, recipient) {
			out = append(out, recipient)
		}
	}
	return out
}

// keepEditedRecipient adds back the person an edited route already names, so
// the form shows who it is rather than silently picking another target.
func (s *Server) keepEditedRecipient(ctx context.Context, recipients []models.Recipient, id string) []models.Recipient {
	if id == "" || slices.ContainsFunc(recipients, func(r models.Recipient) bool { return r.ID == id }) {
		return recipients
	}
	recipient, err := s.store.GetRecipient(ctx, id)
	if err != nil {
		return recipients
	}
	return append(recipients, recipient)
}
