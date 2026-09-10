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

// Reason says how a route was chosen. Answering "which route would this alert
// take?" needs it: a route alone does not say whether its selector matched or
// the default caught the alert.
type Reason string

const (
	ReasonSelector Reason = "selector"
	ReasonDefault  Reason = "default"
	ReasonNone     Reason = "none"
	ReasonNoRoutes Reason = "no-routes"
)

// Match is the routing rule in one place: highest priority first, ties by name,
// a selector that matches every label it names, and the default as the last
// resort. Only a failure to read the store is an error; finding nothing is an
// answer.
func (r *Router) Match(labels map[string]string) (models.Route, Reason, error) {
	routes, err := r.store.ListRoutes()
	if err != nil {
		return models.Route{}, "", err
	}
	if len(routes) == 0 {
		return models.Route{}, ReasonNoRoutes, nil
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
			return route, ReasonSelector, nil
		}
	}

	for _, route := range routes {
		if route.IsDefault {
			return route, ReasonDefault, nil
		}
	}

	return models.Route{}, ReasonNone, nil
}

func (r *Router) SelectRoute(labels map[string]string) (models.Route, error) {
	route, reason, err := r.Match(labels)
	if err != nil {
		return models.Route{}, err
	}

	switch reason {
	case ReasonNoRoutes:
		return models.Route{}, errors.New("no routes configured")
	case ReasonNone:
		return models.Route{}, errors.New("no matching route and no default route")
	}
	return route, nil
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
