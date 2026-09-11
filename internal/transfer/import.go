package transfer

import (
	"errors"
	"fmt"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// A Mode says what an import does with what the bundle does not mention.
type Mode string

const (
	// ModeMerge upserts what the bundle carries and leaves the rest alone.
	ModeMerge Mode = "merge"
	// ModeReplace makes the installation match the bundle, deleting the rest.
	ModeReplace Mode = "replace"
)

func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case ModeMerge, "":
		return ModeMerge, nil
	case ModeReplace:
		return ModeReplace, nil
	default:
		return "", fmt.Errorf("import mode %q is neither merge nor replace", value)
	}
}

// A Change is one thing an import would do. The diff is what a dry run returns
// and what a real import reports having done, so the two cannot describe the
// same import differently.
type Change struct {
	Kind   string `json:"kind"`
	Action string `json:"action"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

// A Result is the whole diff, plus the destinations whose Team or channel the
// bundle names but this installation cannot resolve.
type Result struct {
	Mode       Mode     `json:"mode"`
	DryRun     bool     `json:"dry_run"`
	Changes    []Change `json:"changes"`
	Unresolved []string `json:"unresolved,omitempty"`
}

const (
	actionCreate = "create"
	actionUpdate = "update"
	actionDelete = "delete"
)

// Import applies a bundle. It validates first, so a rejected bundle writes
// nothing, and it runs inside one transaction, so a failure part way through
// leaves the configuration as it found it. A dry run computes the same diff and
// then rolls back rather than taking a different path — a preview that does not
// exercise the real code is a preview of something else.
func Import(st store.Store, bundle Bundle, mode Mode, dryRun bool) (Result, error) {
	if err := bundle.Validate(); err != nil {
		return Result{}, err
	}

	result := Result{Mode: mode, DryRun: dryRun}
	errDryRun := errors.New("dry run")

	err := st.WithTx(func(tx store.Store) error {
		changes, err := apply(tx, bundle, mode)
		if err != nil {
			return err
		}
		result.Changes = changes

		if dryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return Result{}, err
	}

	return result, nil
}

// apply writes the bundle and reports what it did. Order matters: templates and
// destinations exist before the routes that point at them, and routes are
// written parents first so a child never references a row that is not there
// yet.
func apply(tx store.Store, bundle Bundle, mode Mode) ([]Change, error) {
	var changes []Change

	templates, err := tx.ListTemplates()
	if err != nil {
		return nil, err
	}
	existingTemplates := index(templates, func(t models.Template) string { return t.ID })

	for _, template := range bundle.Templates {
		action := actionCreate
		if _, found := existingTemplates[template.ID]; found {
			action = actionUpdate
			if _, err := tx.UpdateTemplate(template); err != nil {
				return nil, err
			}
		} else if _, err := tx.CreateTemplate(template); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "template", Action: action, ID: template.ID, Name: template.Name})
	}

	destinations, err := tx.ListDestinations()
	if err != nil {
		return nil, err
	}
	existingDestinations := index(destinations, func(d models.Destination) string { return d.ID })

	for _, destination := range bundle.DestinationModels() {
		action := actionCreate
		if _, found := existingDestinations[destination.ID]; found {
			action = actionUpdate
			if _, err := tx.UpdateDestination(destination); err != nil {
				return nil, err
			}
		} else if _, err := tx.CreateDestination(destination); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "destination", Action: action, ID: destination.ID, Name: destination.Name})
	}

	routes, err := tx.ListRoutes()
	if err != nil {
		return nil, err
	}
	existingRoutes := index(routes, func(r models.Route) string { return r.ID })

	for _, route := range parentsFirst(bundle.Routes) {
		action := actionCreate
		if _, found := existingRoutes[route.ID]; found {
			action = actionUpdate
			if _, err := tx.UpdateRoute(route); err != nil {
				return nil, err
			}
		} else if _, err := tx.CreateRoute(route); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "route", Action: action, ID: route.ID, Name: route.Name})
	}

	grants, err := tx.ListGrants()
	if err != nil {
		return nil, err
	}
	existingGrants := index(grants, func(g models.Grant) string { return g.ID })

	for _, grant := range bundle.Grants {
		if _, found := existingGrants[grant.ID]; found {
			// A grant is its scope: there is nothing to update that is not the
			// whole row, so an unchanged one is left alone.
			continue
		}
		if _, err := tx.CreateGrant(grant); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "grant", Action: actionCreate, ID: grant.ID, Name: grant.Role})
	}

	if mode != ModeReplace {
		return changes, nil
	}

	// Deletions run children first, so removing a parent never orphans a route
	// that is about to go as well.
	removals, err := deleteUnmentioned(tx, bundle, existingTemplates, existingDestinations, existingRoutes, existingGrants)
	if err != nil {
		return nil, err
	}
	return append(changes, removals...), nil
}

func deleteUnmentioned(
	tx store.Store,
	bundle Bundle,
	templates map[string]models.Template,
	destinations map[string]models.Destination,
	routes map[string]models.Route,
	grants map[string]models.Grant,
) ([]Change, error) {
	var changes []Change

	keepRoutes := idsOf(bundle.Routes, func(r models.Route) string { return r.ID })
	doomed := make([]models.Route, 0, len(routes))
	for _, entry := range byID(routes) {
		if !keepRoutes[entry.ID] {
			doomed = append(doomed, entry.Value)
		}
	}
	for _, route := range childrenFirst(doomed) {
		if err := tx.DeleteRoute(route.ID); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "route", Action: actionDelete, ID: route.ID, Name: route.Name})
	}

	keepGrants := idsOf(bundle.Grants, func(g models.Grant) string { return g.ID })
	for _, entry := range byID(grants) {
		if keepGrants[entry.ID] {
			continue
		}
		if err := tx.DeleteGrant(entry.ID); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "grant", Action: actionDelete, ID: entry.ID, Name: entry.Value.Role})
	}

	keepDestinations := idsOf(bundle.Destinations, func(d BundleDestination) string { return d.ID })
	for _, entry := range byID(destinations) {
		if keepDestinations[entry.ID] {
			continue
		}
		if err := tx.DeleteDestination(entry.ID); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "destination", Action: actionDelete, ID: entry.ID, Name: entry.Value.Name})
	}

	keepTemplates := idsOf(bundle.Templates, func(t models.Template) string { return t.ID })
	for _, entry := range byID(templates) {
		if keepTemplates[entry.ID] {
			continue
		}
		if err := tx.DeleteTemplate(entry.ID); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: "template", Action: actionDelete, ID: entry.ID, Name: entry.Value.Name})
	}

	return changes, nil
}

// parentsFirst orders routes so that a parent is written before its children,
// which is what the tree validation on write expects to find.
func parentsFirst(routes []models.Route) []models.Route {
	remaining := append([]models.Route(nil), routes...)
	sort.SliceStable(remaining, func(i, j int) bool { return remaining[i].ID < remaining[j].ID })

	written := map[string]bool{}
	ordered := make([]models.Route, 0, len(remaining))
	for len(remaining) > 0 {
		progress := false
		next := remaining[:0]
		for _, route := range remaining {
			if route.ParentID == "" || written[route.ParentID] {
				ordered = append(ordered, route)
				written[route.ID] = true
				progress = true
				continue
			}
			next = append(next, route)
		}
		remaining = next

		// A parent outside the bundle cannot be waited for. Validation rejects a
		// cycle, so this is the dangling case, and the route goes as it is.
		if !progress {
			ordered = append(ordered, remaining...)
			break
		}
	}
	return ordered
}

func childrenFirst(routes []models.Route) []models.Route {
	ordered := parentsFirst(routes)
	reversed := make([]models.Route, 0, len(ordered))
	for i := len(ordered) - 1; i >= 0; i-- {
		reversed = append(reversed, ordered[i])
	}
	return reversed
}

func index[T any](items []T, id func(T) string) map[string]T {
	out := make(map[string]T, len(items))
	for _, item := range items {
		out[id(item)] = item
	}
	return out
}

func idsOf[T any](items []T, id func(T) string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[id(item)] = true
	}
	return out
}

// byID returns a map's entries in id order. Map iteration is random, and a diff
// that lists the same deletions in a different order every run is one nobody
// can review.
func byID[T any](items map[string]T) []struct {
	ID    string
	Value T
} {
	out := make([]struct {
		ID    string
		Value T
	}, 0, len(items))
	for id, value := range items {
		out = append(out, struct {
			ID    string
			Value T
		}{ID: id, Value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
