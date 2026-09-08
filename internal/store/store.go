package store

import (
	"errors"

	"github.com/pflege-de-labs/teamster/internal/models"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	Close() error

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

	UpsertActiveAlert(a models.ActiveAlert) error
	GetActiveAlert(fingerprint string) (models.ActiveAlert, error)
	DeleteActiveAlert(fingerprint string) error
}
