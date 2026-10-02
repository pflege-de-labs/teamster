package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/people"
	"github.com/pflege-de-labs/teamster/internal/store"
)

var (
	// errMayNotAddress is a message naming people with a token whose scope,
	// or whose creator, does not allow it (ADR 0082).
	errMayNotAddress = errors.New("this token may not name recipients: recipients and the teamster_recipient label need a token whose message scope allows it")
	// errOnlySelf is a self-only token naming someone other than its creator.
	errOnlySelf = errors.New("this token may only name its creator as a recipient")
)

// authorizeAddresses refuses events that name people their sender may not
// message, before anything is delivered. Without addresses there is nothing
// to ask.
func (s *Server) authorizeAddresses(ctx context.Context, from sender, events []models.Event) error {
	var addresses []string
	for _, ev := range events {
		addresses = append(addresses, models.AddressesOf(ev)...)
	}
	broadcast := isBroadcast(events)
	if len(addresses) == 0 && !broadcast {
		return nil
	}
	if from.creator == nil {
		if broadcast {
			return errMayNotBroadcast
		}
		return errMayNotAddress
	}
	snapshot, err := s.engine.Authorizer(ctx)
	if err != nil {
		return err
	}
	if broadcast {
		if snapshot.AllowTokenMessage(from.token.ID, *from.creator, authz.ActionBroadcast) {
			return nil
		}
		return errMayNotBroadcast
	}
	if snapshot.AllowTokenMessage(from.token.ID, *from.creator, authz.ActionMessage) {
		return nil
	}
	if !snapshot.AllowTokenMessage(from.token.ID, *from.creator, authz.ActionMessageSelf) {
		return errMayNotAddress
	}
	self, err := s.objectIDOf(ctx, from.creator.Subject)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		ok, err := s.isPerson(ctx, address, self)
		if err != nil {
			return err
		}
		if !ok {
			return errOnlySelf
		}
	}
	return nil
}

// objectIDOf is the Entra object id a subject signed in with, or the one their
// linked chat recorded; empty when neither is known.
func (s *Server) objectIDOf(ctx context.Context, subject string) (string, error) {
	user, err := s.store.GetUser(ctx, subject)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if user.ObjectID != "" {
		return user.ObjectID, nil
	}
	recipient, err := s.store.GetRecipientBySubject(ctx, subject)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return recipient.AADObjectID, nil
}

// isPerson reports whether an address names the person with this object id.
// An address that resolves to nobody names nobody; one that cannot be
// resolved right now is an error, so the sender retries.
func (s *Server) isPerson(ctx context.Context, address, objectID string) (bool, error) {
	if objectID == "" {
		return false, nil
	}
	if strings.EqualFold(address, objectID) {
		return true, nil
	}
	if s.people == nil {
		return false, nil
	}
	u, err := s.people.Resolve(ctx, address)
	if err != nil {
		if people.Reason(err) != "" {
			return false, nil
		}
		return false, fmt.Errorf("recipient %s: %w", address, err)
	}
	return strings.EqualFold(u.AADObjectID, objectID), nil
}

// refuseAddresses answers a request authorizeAddresses refused, and reports
// whether it did.
func (s *Server) refuseAddresses(w http.ResponseWriter, r *http.Request, from sender, source string, err error) bool {
	if err == nil {
		return false
	}
	ctx := r.Context()
	if errors.Is(err, errMayNotAddress) || errors.Is(err, errOnlySelf) || errors.Is(err, errMayNotBroadcast) {
		s.metrics.WebhookReceived(ctx, source, "forbidden")
		logging.FromContext(ctx).Warn("webhook refused", "source", source, "token", from.token.Name, "creator", from.token.CreatedBy, "reason", err)
		writeJSONError(w, http.StatusForbidden, err.Error())
		return true
	}
	writeReport(w, r, report{}, err)
	return true
}
