package store

import (
	"context"
	"errors"

	"github.com/pflege-de-labs/teamster/internal/models"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	Close() error
	// Ping reports whether the database can still be reached, which is what
	// readiness turns on.
	Ping(ctx context.Context) error

	// WithTx runs fn against a store bound to one transaction. Everything fn
	// writes lands together or not at all, which is what an import that may be
	// rejected half way through needs.
	//
	// fn receives the context rather than closing over one, so a transaction
	// can be given its own deadline without touching every caller.
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error

	ListTemplates(ctx context.Context) ([]models.Template, error)
	CreateTemplate(ctx context.Context, t models.Template) (models.Template, error)
	UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error)
	DeleteTemplate(ctx context.Context, id string) error
	GetTemplate(ctx context.Context, id string) (models.Template, error)

	ListDestinations(ctx context.Context) ([]models.Destination, error)
	CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error)
	UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error)
	DeleteDestination(ctx context.Context, id string) error
	GetDestination(ctx context.Context, id string) (models.Destination, error)

	ListRoutes(ctx context.Context) ([]models.Route, error)
	CreateRoute(ctx context.Context, r models.Route) (models.Route, error)
	UpdateRoute(ctx context.Context, r models.Route) (models.Route, error)
	DeleteRoute(ctx context.Context, id string) error
	GetRoute(ctx context.Context, id string) (models.Route, error)

	ListGrants(ctx context.Context) ([]models.Grant, error)
	CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error)
	DeleteGrant(ctx context.Context, id string) error

	CreateSession(ctx context.Context, s models.Session) error
	GetSession(ctx context.Context, id string) (models.Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context) error

	CreateLoginFlow(ctx context.Context, f models.LoginFlow) error
	TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error)

	UpsertActiveAlert(ctx context.Context, a models.ActiveAlert) error
	ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error)
	// CountActiveAlerts is how many cards this service is currently keeping up
	// to date. It runs on every metrics collection, so it counts rather than
	// reads.
	CountActiveAlerts(ctx context.Context) (int64, error)
	GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error)
	DeleteActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) error
}
