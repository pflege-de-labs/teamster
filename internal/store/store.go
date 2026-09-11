package store

import (
	"errors"

	"github.com/pflege-de-labs/teamster/internal/models"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	Close() error
	// Ping reports whether the database can still be reached, which is what
	// readiness turns on.
	Ping() error

	ListTemplates() ([]models.Template, error)
	CreateTemplate(t models.Template) (models.Template, error)
	UpdateTemplate(t models.Template) (models.Template, error)
	DeleteTemplate(id string) error
	GetTemplate(id string) (models.Template, error)

	ListDestinations() ([]models.Destination, error)
	CreateDestination(d models.Destination) (models.Destination, error)
	UpdateDestination(d models.Destination) (models.Destination, error)
	DeleteDestination(id string) error
	GetDestination(id string) (models.Destination, error)

	ListRoutes() ([]models.Route, error)
	CreateRoute(r models.Route) (models.Route, error)
	UpdateRoute(r models.Route) (models.Route, error)
	DeleteRoute(id string) error
	GetRoute(id string) (models.Route, error)

	ListGrants() ([]models.Grant, error)
	CreateGrant(g models.Grant) (models.Grant, error)
	DeleteGrant(id string) error

	CreateSession(s models.Session) error
	GetSession(id string) (models.Session, error)
	DeleteSession(id string) error
	DeleteExpiredSessions() error

	CreateLoginFlow(f models.LoginFlow) error
	TakeLoginFlow(state string) (models.LoginFlow, error)

	UpsertActiveAlert(a models.ActiveAlert) error
	ListActiveAlerts(fingerprint string) ([]models.ActiveAlert, error)
	GetActiveAlert(fingerprint, teamID, channelID string) (models.ActiveAlert, error)
	DeleteActiveAlert(fingerprint, teamID, channelID string) error
}
