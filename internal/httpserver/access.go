package httpserver

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errNotAllowed is what a refused record check wraps, so callers answer 403.
var errNotAllowed = errors.New("not allowed")

// refusal says what was refused, in words a form can show.
type refusal struct{ action, typ string }

func (e refusal) Error() string {
	return "you may not " + e.action + " this " + strings.ToLower(e.typ)
}

func (e refusal) Is(target error) bool { return target == errNotAllowed }

type checkedKey struct{}

// markChecked tells the witness the handler asked about a record.
func markChecked(r *http.Request) {
	if checked, ok := r.Context().Value(checkedKey{}).(*atomic.Bool); ok {
		checked.Store(true)
	}
}

// can asks about one record: id "" is the collection, where create is asked.
func (s *Server) can(r *http.Request, action, typ, id string) bool {
	markChecked(r)
	return s.allow(r, action, authz.Resource{Type: typ, ID: id})
}

// mayRecord is can as an error a form shows and the API answers with 403.
func (s *Server) mayRecord(r *http.Request, action, typ, id string) error {
	if s.can(r, action, typ, id) {
		return nil
	}
	return userError{refusal{action: action, typ: typ}}
}

// readable keeps the records the request may read. A role that reads the whole
// collection skips the per-record checks.
func readable[T any](s *Server, r *http.Request, typ string, items []T, id func(T) string) []T {
	markChecked(r)
	if s.allow(r, authz.ActionRead, authz.Resource{Type: typ}) {
		return items
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		if s.allow(r, authz.ActionRead, authz.Resource{Type: typ, ID: id(item)}) {
			out = append(out, item)
		}
	}
	return out
}

// permissionedType is a resource whose records carry their own permissions (ADR 0075).
func permissionedType(typ string) bool {
	return slices.Contains(authz.PermissionedTypes, typ)
}

// createOwned runs create in a transaction and makes the caller the owner of
// what it made, in the same commit.
func (s *Server) createOwned(ctx context.Context, r *http.Request, typ string, create func(ctx context.Context, tx store.Store) (string, error)) error {
	return s.store.WithTx(ctx, func(ctx context.Context, tx store.Store) error {
		id, err := create(ctx, tx)
		if err != nil {
			return err
		}
		return grantOwner(ctx, tx, r, typ, id)
	})
}

func grantOwner(ctx context.Context, tx store.Store, r *http.Request, typ, id string) error {
	subject := principalSubject(r)
	if subject == "" {
		return nil
	}
	_, err := tx.PutPermission(ctx, models.Permission{
		PrincipalType: models.PrincipalUser, PrincipalID: subject,
		ResourceType: typ, ResourceID: id,
		Actions: []string{authz.ActionOwn}, CreatedBy: subject,
	})
	return err
}

// deleteOwned deletes a record and who may do what with it, together.
func (s *Server) deleteOwned(ctx context.Context, typ, id string, del func(ctx context.Context, tx store.Store) error) error {
	return s.store.WithTx(ctx, func(ctx context.Context, tx store.Store) error {
		if err := del(ctx, tx); err != nil {
			return err
		}
		return tx.DeletePermissionsFor(ctx, typ, id)
	})
}

// recordAccess is what the page's per-row controls ask about.
func (s *Server) recordAccess(r *http.Request, typ, id string) recordFlags {
	return recordFlags{
		Update: s.allow(r, authz.ActionUpdate, authz.Resource{Type: typ, ID: id}),
		Delete: s.allow(r, authz.ActionDelete, authz.Resource{Type: typ, ID: id}),
		Share:  s.allow(r, authz.ActionShare, authz.Resource{Type: typ, ID: id}),
	}
}

type recordFlags struct{ Update, Delete, Share bool }

// writeRecordError answers a refused check with 403 and a missing record with 404.
func writeRecordError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNotAllowed):
		writeError(w, r, http.StatusForbidden, err)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, err)
	default:
		writeError(w, r, http.StatusInternalServerError, err)
	}
}

// Resource types with their own permissions, as Cedar names them.
const (
	typeTemplate        = "Template"
	typeDestination     = "Destination"
	typeRoute           = "Route"
	typeWebhookEndpoint = "WebhookEndpoint"
	typeGroup           = "Group"
)

func templateID(t models.Template) string        { return t.ID }
func destinationID(d models.Destination) string  { return d.ID }
func routeID(rt models.Route) string             { return rt.ID }
func endpointID(e models.WebhookEndpoint) string { return e.ID }
func groupID(g models.Group) string              { return g.ID }

// recordAction is what a method asks of one record.
func recordAction(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return authz.ActionRead
	case http.MethodDelete:
		return authz.ActionDelete
	default:
		return authz.ActionUpdate
	}
}

func (s *Server) createTemplate(r *http.Request, t models.Template) (models.Template, error) {
	var created models.Template
	err := s.createOwned(r.Context(), r, typeTemplate, func(ctx context.Context, tx store.Store) (string, error) {
		var err error
		created, err = tx.CreateTemplate(ctx, t)
		return created.ID, err
	})
	return created, err
}

func (s *Server) createDestination(r *http.Request, d models.Destination) (models.Destination, error) {
	var created models.Destination
	err := s.createOwned(r.Context(), r, typeDestination, func(ctx context.Context, tx store.Store) (string, error) {
		var err error
		created, err = tx.CreateDestination(ctx, d)
		return created.ID, err
	})
	return created, err
}

// mayRepointDestination is mayDeliverTo, except that someone whose update comes
// from a grant on this destination rather than from a role may keep its
// channel: their grant is the scope. Moving it takes the delivery scope.
func (s *Server) mayRepointDestination(r *http.Request, d models.Destination) (bool, error) {
	if !s.allow(r, authz.ActionEdit, authz.Resource{Type: typeDestination}) {
		existing, err := s.store.GetDestination(r.Context(), d.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return false, err
		}
		if err == nil && existing.TeamID == d.TeamID && existing.ChannelID == d.ChannelID {
			return true, nil
		}
	}
	return s.mayDeliverTo(r, d.TeamID, d.ChannelID)
}

// listDestinations keeps the channel scoping a role reads under, and shows a
// grant holder the destinations shared with them.
func (s *Server) listDestinations(r *http.Request, items []models.Destination) ([]models.Destination, error) {
	markChecked(r)
	if s.allow(r, authz.ActionRead, authz.Resource{Type: typeDestination}) {
		return s.visibleDestinations(r, items)
	}
	return readable(s, r, typeDestination, items, destinationID), nil
}

// listEndpoints is listDestinations for webhook endpoints.
func (s *Server) listEndpoints(r *http.Request, items []models.WebhookEndpoint) ([]models.WebhookEndpoint, error) {
	markChecked(r)
	if s.allow(r, authz.ActionRead, authz.Resource{Type: typeWebhookEndpoint}) {
		return s.visibleWebhookEndpoints(r, items)
	}
	return readable(s, r, typeWebhookEndpoint, items, endpointID), nil
}

func (s *Server) createEndpoint(r *http.Request, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	var created models.WebhookEndpoint
	err := s.createOwned(r.Context(), r, typeWebhookEndpoint, func(ctx context.Context, tx store.Store) (string, error) {
		var err error
		created, err = tx.CreateWebhookEndpoint(ctx, e)
		return created.ID, err
	})
	return created, err
}

func (s *Server) deleteEndpoint(r *http.Request, id string) error {
	return s.deleteOwned(r.Context(), typeWebhookEndpoint, id, func(ctx context.Context, tx store.Store) error {
		return tx.DeleteWebhookEndpoint(ctx, id)
	})
}

// mayAttachTemplate is whether a route or endpoint may render with this template.
func (s *Server) mayAttachTemplate(r *http.Request, templateID string) bool {
	return templateID == "" || s.can(r, authz.ActionAttach, typeTemplate, templateID)
}
