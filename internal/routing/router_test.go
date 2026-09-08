package routing

import (
	"testing"

	"github.com/pflege-de/teamster/internal/models"
	"github.com/pflege-de/teamster/internal/store"
)

type stubStore struct {
	routes []models.Route
	err    error
}

func (s stubStore) Close() error { return nil }

func (s stubStore) ListTemplates() ([]models.Template, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateTemplate(t models.Template) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) UpdateTemplate(t models.Template) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) DeleteTemplate(id string) error { return store.ErrNotFound }

func (s stubStore) GetTemplate(id string) (models.Template, error) {
	return models.Template{}, store.ErrNotFound
}

func (s stubStore) ListDestinations() ([]models.Destination, error) {
	return nil, store.ErrNotFound
}

func (s stubStore) CreateDestination(d models.Destination) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) UpdateDestination(d models.Destination) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) DeleteDestination(id string) error { return store.ErrNotFound }

func (s stubStore) GetDestination(id string) (models.Destination, error) {
	return models.Destination{}, store.ErrNotFound
}

func (s stubStore) ListRoutes() ([]models.Route, error) {
	return s.routes, s.err
}

func (s stubStore) CreateRoute(r models.Route) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) UpdateRoute(r models.Route) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) DeleteRoute(id string) error { return store.ErrNotFound }

func (s stubStore) GetRoute(id string) (models.Route, error) {
	return models.Route{}, store.ErrNotFound
}

func (s stubStore) UpsertActiveAlert(a models.ActiveAlert) error { return store.ErrNotFound }

func (s stubStore) GetActiveAlert(fingerprint string) (models.ActiveAlert, error) {
	return models.ActiveAlert{}, store.ErrNotFound
}

func (s stubStore) DeleteActiveAlert(fingerprint string) error { return store.ErrNotFound }

func TestSelectRoute_MatchPriority(t *testing.T) {
	routes := []models.Route{
		{
			ID:            "r1",
			Name:          "default",
			IsDefault:     true,
			DestinationID: "d1",
			TemplateID:    "t1",
			Priority:      1,
		},
		{
			ID:            "r2",
			Name:          "critical",
			LabelSelector: map[string]string{"severity": "critical"},
			DestinationID: "d2",
			TemplateID:    "t2",
			Priority:      50,
		},
		{
			ID:            "r3",
			Name:          "critical-high",
			LabelSelector: map[string]string{"severity": "critical", "tier": "gold"},
			DestinationID: "d3",
			TemplateID:    "t3",
			Priority:      100,
		},
	}

	router := New(stubStore{routes: routes})

	selected, err := router.SelectRoute(map[string]string{"severity": "critical", "tier": "gold"})
	if err != nil {
		t.Fatalf("SelectRoute returned error: %v", err)
	}
	if selected.ID != "r3" {
		t.Fatalf("expected r3, got %s", selected.ID)
	}
}

func TestSelectRoute_DefaultFallback(t *testing.T) {
	routes := []models.Route{
		{
			ID:            "r1",
			Name:          "default",
			IsDefault:     true,
			DestinationID: "d1",
			TemplateID:    "t1",
			Priority:      1,
		},
		{
			ID:            "r2",
			Name:          "critical",
			LabelSelector: map[string]string{"severity": "critical"},
			DestinationID: "d2",
			TemplateID:    "t2",
			Priority:      50,
		},
	}

	router := New(stubStore{routes: routes})

	selected, err := router.SelectRoute(map[string]string{"severity": "warning"})
	if err != nil {
		t.Fatalf("SelectRoute returned error: %v", err)
	}
	if selected.ID != "r1" {
		t.Fatalf("expected r1, got %s", selected.ID)
	}
}

func TestSelectRoute_NoRoutes(t *testing.T) {
	router := New(stubStore{routes: nil})

	_, err := router.SelectRoute(map[string]string{"severity": "critical"})
	if err == nil {
		t.Fatal("expected error when no routes configured")
	}
}
