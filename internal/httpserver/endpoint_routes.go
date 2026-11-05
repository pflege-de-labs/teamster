package httpserver

import (
	"context"
	"errors"
	"net/http"

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
	want := endpointRoute(e)
	if !found {
		created, err := tx.CreateRoute(ctx, want)
		if err != nil {
			return err
		}
		return grantOwner(ctx, tx, r, typeRoute, created.ID)
	}

	// The selector is locked, so it still names the previous slugs.
	oldName := endpointRouteName(current.LabelSelector[models.TeamLabel], current.LabelSelector[models.ChannelLabel])
	if current.Name != oldName {
		want.Name = current.Name
	}
	want.ID, want.Priority, want.Greedy = current.ID, current.Priority, current.Greedy
	_, err = tx.UpdateRoute(ctx, want)
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
