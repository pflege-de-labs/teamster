package httpserver

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errEndpointRouteHasChildren refuses deleting a webhook whose route others hang under.
var errEndpointRouteHasChildren = errors.New("webhook route has child routes")

func endpointRouteName(team, channel string) string { return "webhook " + team + "/" + channel }

// endpointRoute is the route that sends an endpoint's messages to its destination.
func endpointRoute(e models.WebhookEndpoint) models.Route {
	return models.Route{
		Name: endpointRouteName(e.TeamSlug, e.ChannelSlug),
		LabelSelector: map[string]string{
			models.SourceLabel:  models.SourceTeamsV2,
			models.TeamLabel:    e.TeamSlug,
			models.ChannelLabel: e.ChannelSlug,
		},
		DestinationID:     e.DestinationID,
		TemplateID:        e.TemplateID,
		WebhookEndpointID: e.ID,
		// A greedy child would silence the endpoint's own channel (ADR 0096).
		NoGreedyChildren: true,
	}
}

func linkedRoute(ctx context.Context, tx store.Store, endpointID string) (models.Route, bool, error) {
	routes, err := tx.ListRoutes(ctx)
	if err != nil {
		return models.Route{}, false, err
	}
	for _, route := range routes {
		if route.WebhookEndpointID == endpointID {
			return route, true, nil
		}
	}
	return models.Route{}, false, nil
}

// syncEndpointRoute creates the endpoint's route or brings the existing one in line.
// It bypasses saveRouteChecked on purpose: that path locks linked routes.
func syncEndpointRoute(ctx context.Context, tx store.Store, r *http.Request, e models.WebhookEndpoint) error {
	current, found, err := linkedRoute(ctx, tx, e.ID)
	if err != nil {
		return err
	}
	if !found {
		created, err := tx.CreateRoute(ctx, endpointRoute(e))
		if err != nil {
			return err
		}
		return grantOwner(ctx, tx, r, typeRoute, created.ID)
	}
	return alignEndpointRoute(ctx, tx, current, e)
}

// alignEndpointRoute rewrites the locked fields of an endpoint's route, keeping name, priority and greedy.
func alignEndpointRoute(ctx context.Context, tx store.Store, current models.Route, e models.WebhookEndpoint) error {
	want := endpointRoute(e)
	// The selector is locked, so it still names the previous slugs.
	oldName := endpointRouteName(current.LabelSelector[models.TeamLabel], current.LabelSelector[models.ChannelLabel])
	if current.Name != oldName {
		want.Name = current.Name
	}
	want.ID, want.Priority, want.Greedy = current.ID, current.Priority, current.Greedy
	_, err := tx.UpdateRoute(ctx, want)
	return err
}

func (s *Server) updateEndpoint(r *http.Request, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	var updated models.WebhookEndpoint
	err := s.store.WithTx(r.Context(), func(ctx context.Context, tx store.Store) error {
		var err error
		if updated, err = tx.UpdateWebhookEndpoint(ctx, e); err != nil {
			return err
		}
		return syncEndpointRoute(ctx, tx, r, updated)
	})
	return updated, err
}

// deleteEndpointRoute removes the endpoint's route and its permissions, or refuses while it has children.
func deleteEndpointRoute(ctx context.Context, tx store.Store, endpointID string) error {
	route, found, err := linkedRoute(ctx, tx, endpointID)
	if err != nil || !found {
		return err
	}
	routes, err := tx.ListRoutes(ctx)
	if err != nil {
		return err
	}
	if err := routing.ValidateDelete(route.ID, routes); err != nil {
		return invalidRoute{errors.Join(errEndpointRouteHasChildren, err)}
	}
	if err := tx.DeleteRoute(ctx, route.ID); err != nil {
		return err
	}
	return tx.DeletePermissionsFor(ctx, typeRoute, route.ID)
}

// Reconciled counts what ReconcileEndpointRoutes changed.
type Reconciled struct{ Created, Updated, Deleted, Unlinked int }

// ReconcileEndpointRoutes gives every endpoint its linked route, repairs one the previous release
// edited, and cleans up links it left dangling. It is idempotent and applies all or nothing,
// serializably because deleting versus unlinking an orphan depends on its children.
func ReconcileEndpointRoutes(ctx context.Context, st store.Store) (Reconciled, error) {
	var done Reconciled
	err := st.WithSerializableTx(ctx, func(ctx context.Context, tx store.Store) error {
		done = Reconciled{}
		endpoints, err := tx.ListWebhookEndpoints(ctx)
		if err != nil {
			return err
		}
		routes, err := tx.ListRoutes(ctx)
		if err != nil {
			return err
		}
		exists := make(map[string]bool, len(endpoints))
		for _, e := range endpoints {
			exists[e.ID] = true
		}
		linked := make(map[string]models.Route, len(routes))
		parents := make(map[string]bool, len(routes))
		for _, r := range routes {
			parents[r.ParentID] = true
			if r.WebhookEndpointID != "" && exists[r.WebhookEndpointID] {
				linked[r.WebhookEndpointID] = r
			}
		}

		for _, r := range routes {
			if r.WebhookEndpointID == "" || exists[r.WebhookEndpointID] {
				continue
			}
			if parents[r.ID] {
				if err := tx.UnlinkRoute(ctx, r.ID); err != nil {
					return err
				}
				done.Unlinked++
				continue
			}
			if err := tx.DeleteRoute(ctx, r.ID); err != nil {
				return err
			}
			if err := tx.DeletePermissionsFor(ctx, typeRoute, r.ID); err != nil {
				return err
			}
			done.Deleted++
		}

		for _, e := range endpoints {
			if current, ok := linked[e.ID]; ok {
				// The previous release did not lock the route, so it may have been edited.
				if !lockedFieldsChanged(current, endpointRoute(e)) {
					continue
				}
				if err := alignEndpointRoute(ctx, tx, current, e); err != nil {
					return err
				}
				done.Updated++
				continue
			}
			created, err := tx.CreateRoute(ctx, endpointRoute(e))
			if err != nil {
				return err
			}
			perms, err := tx.ListPermissionsFor(ctx, typeWebhookEndpoint, e.ID)
			if err != nil {
				return err
			}
			for _, p := range perms {
				if !slices.Contains(p.Actions, authz.ActionOwn) {
					continue
				}
				if _, err := tx.PutPermission(ctx, models.Permission{
					PrincipalType: p.PrincipalType, PrincipalID: p.PrincipalID,
					ResourceType: typeRoute, ResourceID: created.ID,
					Actions: []string{authz.ActionOwn}, CreatedBy: p.CreatedBy,
				}); err != nil {
					return err
				}
			}
			done.Created++
		}
		return nil
	})
	if err != nil {
		return Reconciled{}, err
	}
	return done, nil
}
