package routing

import (
	"errors"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

type Router struct {
	store store.Store
}

func New(store store.Store) *Router {
	return &Router{store: store}
}

func (r *Router) SelectRoute(labels map[string]string) (models.Route, error) {
	routes, err := r.store.ListRoutes()
	if err != nil {
		return models.Route{}, err
	}
	if len(routes) == 0 {
		return models.Route{}, errors.New("no routes configured")
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Priority == routes[j].Priority {
			return routes[i].Name < routes[j].Name
		}
		return routes[i].Priority > routes[j].Priority
	})

	for _, route := range routes {
		if route.IsDefault {
			continue
		}
		if labelsMatch(route.LabelSelector, labels) {
			return route, nil
		}
	}

	for _, route := range routes {
		if route.IsDefault {
			return route, nil
		}
	}

	return models.Route{}, errors.New("no matching route and no default route")
}

func labelsMatch(selector map[string]string, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}
