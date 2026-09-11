// Package transfer moves a configuration between installations: templates,
// destinations, routes and permission grants as one JSON document. It holds no
// secrets and no runtime state, so a bundle is safe to keep in a repository
// beside the rest of a deployment's configuration.
package transfer

import (
	"fmt"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// Version is the bundle format. An import refuses a version it does not know
// rather than guessing at fields it has never seen.
const Version = 1

// A Bundle is a whole configuration. Ids are preserved, so re-importing a
// bundle into the installation it came from changes nothing.
type Bundle struct {
	Version      int                 `json:"version"`
	ExportedAt   time.Time           `json:"exported_at"`
	Templates    []models.Template   `json:"templates"`
	Destinations []BundleDestination `json:"destinations"`
	Routes       []models.Route      `json:"routes"`
	Grants       []models.Grant      `json:"grants"`
}

// A BundleDestination is a destination with the Team and channel names beside
// their ids. The ids are tenant-specific, so a bundle carried to another tenant
// points at nothing — the names are what makes that legible to whoever has to
// fix it up.
type BundleDestination struct {
	models.Destination
	TeamName    string `json:"team_name,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
}

// Directory resolves ids to names for an export. It is optional: a bundle
// without names is still a bundle, and Microsoft Graph being unreachable is no
// reason to refuse to back a configuration up.
type Directory interface {
	TeamName(teamID string) string
	ChannelName(teamID, channelID string) string
}

// Export reads the whole configuration. Sessions, login flows and active alerts
// are runtime state and are deliberately absent, as are all credentials: what
// comes back is what an operator configured, and nothing that would be
// dangerous in a backup.
func Export(st store.Store, directory Directory) (Bundle, error) {
	templates, err := st.ListTemplates()
	if err != nil {
		return Bundle{}, fmt.Errorf("templates: %w", err)
	}
	destinations, err := st.ListDestinations()
	if err != nil {
		return Bundle{}, fmt.Errorf("destinations: %w", err)
	}
	routes, err := st.ListRoutes()
	if err != nil {
		return Bundle{}, fmt.Errorf("routes: %w", err)
	}
	grants, err := st.ListGrants()
	if err != nil {
		return Bundle{}, fmt.Errorf("grants: %w", err)
	}

	bundle := Bundle{
		Version:      Version,
		ExportedAt:   time.Now().UTC(),
		Templates:    emptyWhenNil(templates),
		Destinations: make([]BundleDestination, 0, len(destinations)),
		Routes:       emptyWhenNil(routes),
		Grants:       emptyWhenNil(grants),
	}

	for _, destination := range destinations {
		carried := BundleDestination{Destination: destination}
		if directory != nil {
			carried.TeamName = directory.TeamName(destination.TeamID)
			carried.ChannelName = directory.ChannelName(destination.TeamID, destination.ChannelID)
		}
		bundle.Destinations = append(bundle.Destinations, carried)
	}

	return bundle, nil
}

// A JSON null reads worse than an empty list for anyone editing a bundle by
// hand.
func emptyWhenNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// Validate checks a bundle against itself, so an import is refused before it
// has written anything rather than half way through. References may point
// within the bundle only: an import that leaned on what happened to be in the
// database already would behave differently on a fresh installation.
func (b Bundle) Validate() error {
	if b.Version != Version {
		return fmt.Errorf("bundle version %d is not %d, which this build understands", b.Version, Version)
	}

	templates := map[string]bool{}
	for _, template := range b.Templates {
		if template.ID == "" {
			return fmt.Errorf("template %q has no id", template.Name)
		}
		if templates[template.ID] {
			return fmt.Errorf("template id %q appears twice", template.ID)
		}
		templates[template.ID] = true
	}

	destinations := map[string]bool{}
	for _, destination := range b.Destinations {
		if destination.ID == "" {
			return fmt.Errorf("destination %q has no id", destination.Name)
		}
		if destinations[destination.ID] {
			return fmt.Errorf("destination id %q appears twice", destination.ID)
		}
		destinations[destination.ID] = true
	}

	routes := make([]models.Route, 0, len(b.Routes))
	seen := map[string]bool{}
	for _, route := range b.Routes {
		if route.ID == "" {
			return fmt.Errorf("route %q has no id", route.Name)
		}
		if seen[route.ID] {
			return fmt.Errorf("route id %q appears twice", route.ID)
		}
		seen[route.ID] = true

		if route.TemplateID != "" && !templates[route.TemplateID] {
			return fmt.Errorf("route %q renders with template %q, which the bundle does not carry", route.Name, route.TemplateID)
		}
		if route.DestinationID != "" && !destinations[route.DestinationID] {
			return fmt.Errorf("route %q delivers to destination %q, which the bundle does not carry", route.Name, route.DestinationID)
		}
		routes = append(routes, route)
	}

	// The tree rules are routing's, and a bundle carrying a cycle would import
	// a configuration that never delivers.
	for _, route := range routes {
		if err := routing.ValidateRoute(route, routes); err != nil {
			return fmt.Errorf("route %q: %w", route.Name, err)
		}
	}

	grants := map[string]bool{}
	for _, grant := range b.Grants {
		if grant.ID == "" {
			return fmt.Errorf("a grant for role %q has no id", grant.Role)
		}
		// Without this, the second of two grants sharing an id is silently
		// skipped on import and the bundle quietly means less than it says.
		if grants[grant.ID] {
			return fmt.Errorf("grant id %q appears twice", grant.ID)
		}
		grants[grant.ID] = true

		if grant.Role == "" || grant.TeamID == "" {
			return fmt.Errorf("grant %q needs a role and a Team", grant.ID)
		}
	}

	return nil
}

// Destinations drops the names, which are a hint for a human rather than
// anything the store keeps.
func (b Bundle) DestinationModels() []models.Destination {
	out := make([]models.Destination, 0, len(b.Destinations))
	for _, destination := range b.Destinations {
		out = append(out, destination.Destination)
	}
	return out
}
