package routing

import (
	"context"
	"fmt"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// MaxDepth bounds the route tree. Nesting deeper than this is a configuration
// nobody can reason about, and an unbounded walk is a delivery that never ends.
const MaxDepth = 5

type Router struct {
	store store.Store
}

func New(store store.Store) *Router {
	return &Router{store: store}
}

// Reason says how a route was chosen. Answering "which route would this alert
// take?" needs it: a route alone does not say whether its selector matched, the
// default caught the alert, or a child refined a parent that had already
// matched.
type Reason string

const (
	ReasonSelector Reason = "selector"
	ReasonDefault  Reason = "default"
	ReasonRefined  Reason = "refined"
	ReasonNone     Reason = "none"
	ReasonNoRoutes Reason = "no-routes"
)

// A DeliveryKind says which transport a Delivery goes out through, since a
// route may now name a channel, a person, or both (ADR 0026).
type DeliveryKind string

const (
	DeliveryChannel   DeliveryKind = "channel"
	DeliveryRecipient DeliveryKind = "recipient"
)

// A Delivery is one message this alert produces: where it goes and what renders
// it, with the target and template already resolved through inheritance. Kind
// says which of DestinationID or RecipientID is the one that applies; the
// other is left zero.
type Delivery struct {
	RouteID       string       `json:"route_id"`
	RouteName     string       `json:"route_name"`
	Kind          DeliveryKind `json:"kind"`
	DestinationID string       `json:"destination_id,omitempty"`
	RecipientID   string       `json:"recipient_id,omitempty"`
	TemplateID    string       `json:"template_id"`
	Reason        Reason       `json:"reason"`
}

// A Result is what an alert's labels produce: the deliveries, and why the tree
// was entered where it was. The root is named because an explanation of a
// fan-out starts with the route that matched first.
type Result struct {
	Reason     Reason     `json:"reason"`
	RootID     string     `json:"root_id,omitempty"`
	RootName   string     `json:"root_name,omitempty"`
	Deliveries []Delivery `json:"deliveries"`
}

// Plan is the routing rule in one place. A root route is chosen the way it
// always was — highest priority first, ties by name, the default last — and its
// matching children then refine it: a child delivers as well as its parent, or
// instead of it when greedy. Only a failure to read the store is an error;
// finding nothing is an answer.
func (r *Router) Plan(ctx context.Context, labels map[string]string) (Result, error) {
	routes, err := r.store.ListRoutes(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(routes) == 0 {
		return Result{Reason: ReasonNoRoutes}, nil
	}

	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	children := map[string][]models.Route{}
	var roots []models.Route
	for _, route := range routes {
		// A parent that no longer exists would make its children unreachable,
		// so they are treated as roots rather than dropped.
		if _, ok := byID[route.ParentID]; route.ParentID != "" && ok {
			children[route.ParentID] = append(children[route.ParentID], route)
			continue
		}
		roots = append(roots, route)
	}
	sortRoutes(roots)
	for parent := range children {
		sortRoutes(children[parent])
	}

	root, reason := selectRoot(roots, labels)
	if reason == ReasonNone {
		return Result{Reason: ReasonNone}, nil
	}

	plan := collect(root, reason, children, labels, root.DestinationID, root.TemplateID, root.RecipientID, 0)
	if len(plan) == 0 {
		return Result{Reason: ReasonNone}, nil
	}
	return Result{Reason: reason, RootID: root.ID, RootName: root.Name, Deliveries: plan}, nil
}

func selectRoot(roots []models.Route, labels map[string]string) (models.Route, Reason) {
	for _, route := range roots {
		if route.IsDefault {
			continue
		}
		if labelsMatch(route.LabelSelector, labels) {
			return route, ReasonSelector
		}
	}
	for _, route := range roots {
		if route.IsDefault {
			return route, ReasonDefault
		}
	}
	return models.Route{}, ReasonNone
}

// collect walks the matching part of the tree. A route delivers unless one of
// its matching children is greedy, and an unset destination, recipient or
// template is inherited from the nearest ancestor that set one. A route
// fans out to one Delivery per target that ends up set -- a channel, a
// recipient, both or neither -- rather than always producing exactly one, so a
// route with nothing left to deliver to (inherited or its own) delivers
// nothing.
func collect(route models.Route, reason Reason, children map[string][]models.Route, labels map[string]string, destinationID, templateID, recipientID string, depth int) []Delivery {
	if route.DestinationID != "" {
		destinationID = route.DestinationID
	}
	if route.TemplateID != "" {
		templateID = route.TemplateID
	}
	if route.RecipientID != "" {
		recipientID = route.RecipientID
	}

	var (
		fromChildren []Delivery
		suppressed   bool
	)
	if depth < MaxDepth {
		for _, child := range children[route.ID] {
			if !labelsMatch(child.LabelSelector, labels) {
				continue
			}
			if child.Greedy {
				// A greedy child takes both of its parent's deliveries with
				// it, not just the one it happens to share a target kind
				// with -- one flag governs both below.
				suppressed = true
			}
			fromChildren = append(fromChildren, collect(child, ReasonRefined, children, labels, destinationID, templateID, recipientID, depth+1)...)
		}
	}

	plan := make([]Delivery, 0, len(fromChildren)+2)
	if !suppressed {
		if destinationID != "" {
			plan = append(plan, Delivery{
				RouteID:       route.ID,
				RouteName:     route.Name,
				Kind:          DeliveryChannel,
				DestinationID: destinationID,
				TemplateID:    templateID,
				Reason:        reason,
			})
		}
		if recipientID != "" {
			// Channel before recipient, so a mixed plan orders the same way
			// every time and Deliveries[1] means something stable.
			plan = append(plan, Delivery{
				RouteID:     route.ID,
				RouteName:   route.Name,
				Kind:        DeliveryRecipient,
				RecipientID: recipientID,
				TemplateID:  templateID,
				Reason:      reason,
			})
		}
	}
	return append(plan, fromChildren...)
}

// ValidateRoute rejects a tree nobody could deliver through. It takes the routes
// already stored because every rule here is about the candidate's place among
// them, and a cycle found at delivery time is an alert that never arrives.
func ValidateRoute(candidate models.Route, existing []models.Route) error {
	if candidate.ParentID == "" {
		return nil
	}
	if candidate.ParentID == candidate.ID {
		return fmt.Errorf("a route cannot be its own parent")
	}

	byID := map[string]models.Route{}
	for _, route := range existing {
		byID[route.ID] = route
	}
	if _, ok := byID[candidate.ParentID]; !ok {
		return fmt.Errorf("parent route %q does not exist", candidate.ParentID)
	}

	// A child is only reachable once its parent matched, so it needs a selector
	// of its own and something to add.
	if len(candidate.LabelSelector) == 0 {
		return fmt.Errorf("a child route needs a label selector to refine its parent")
	}
	if candidate.DestinationID == "" && candidate.TemplateID == "" && candidate.RecipientID == "" {
		return fmt.Errorf("a child route that inherits its destination, recipient and template changes nothing")
	}
	if candidate.IsDefault {
		return fmt.Errorf("only a root route can be the default")
	}

	depth := 1
	for parent, ok := byID[candidate.ParentID]; ok && parent.ParentID != ""; parent, ok = byID[parent.ParentID] {
		if parent.ParentID == candidate.ID {
			return fmt.Errorf("route %q is already below %q, so this would make a cycle", candidate.ParentID, candidate.Name)
		}
		depth++
		if depth > MaxDepth {
			return fmt.Errorf("route nesting is limited to %d levels", MaxDepth)
		}
	}
	return nil
}

// ValidateDelete keeps a delete from orphaning children, which would silently
// promote them to roots that match every alert their parent used to filter.
func ValidateDelete(id string, existing []models.Route) error {
	for _, route := range existing {
		if route.ParentID == id {
			return fmt.Errorf("route %q still has child routes; remove or reparent them first", id)
		}
	}
	return nil
}

func sortRoutes(routes []models.Route) {
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Priority == routes[j].Priority {
			return routes[i].Name < routes[j].Name
		}
		return routes[i].Priority > routes[j].Priority
	})
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
