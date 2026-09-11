// Package views renders the admin UI. The templ sources next to this file are
// compiled to *_templ.go by `make generate`, and the generated files are
// committed so a plain `go build` needs no extra tooling.
package views

//go:generate go tool templ generate
//go:generate ../../../bin/tailwindcss --input styles.css --output ../web/styles.css --minify

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/cards"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// A Viewer is who is looking at a page: the name the provider gave them and the
// roles they hold. The header shows both, so a missing control has a visible
// reason.
type Viewer struct {
	Name  string
	Roles []string
}

// roleLabel reads the roles the way an operator would say them, and says so
// plainly when there are none — that is the case where the page is empty and
// the reason needs to be in front of them.
func (v Viewer) roleLabel(ctx context.Context) string {
	named := make([]string, 0, len(v.Roles))
	for _, role := range v.Roles {
		if role != "" && role != "none" {
			named = append(named, role)
		}
	}
	if len(named) == 0 {
		return i18n.T(ctx, "header.no_role")
	}
	return strings.Join(named, ", ")
}

// Login carries what the sign-in page needs: which ways in are configured, and
// why the last attempt failed.
type Login struct {
	Error      string
	OIDC       bool
	LocalLogin bool
}

// Page carries everything the admin page renders. The lists come straight from
// the store, and Notice reports the outcome of the last form submission.
type Page struct {
	Templates    []models.Template
	Destinations []models.Destination
	Routes       []models.Route
	Notice       string
	Error        string

	// Sample alerts the preview can render the template against.
	PreviewSamples []string

	// Snippets and Starter are what the card palette inserts.
	Snippets []cards.Snippet
	Starter  string

	// CanEdit hides what this session may not do. Hiding is not enforcing —
	// the server refuses the post either way — but showing a viewer a Save
	// button they cannot use is its own kind of broken.
	CanEdit bool

	// Viewer is who the page is being rendered for, shown in the header.
	Viewer Viewer

	// Grants scope roles to Teams and channels. Only an admin sees or sets them.
	Grants    []models.Grant
	CanManage bool

	// A nil Edit* means the matching form creates rather than updates.
	EditTemplate    *models.Template
	EditDestination *models.Destination
	EditRoute       *models.Route
}

// A routeRow is a route as the list draws it: its place in the tree, and where
// its destination and template came from.
type routeRow struct {
	Route       models.Route
	Depth       int
	Destination inheritedRef
	Template    inheritedRef
}

type inheritedRef struct {
	ID        string
	Inherited bool
}

// routeTree walks the routes depth first, so a child is drawn under the route it
// refines rather than wherever its priority puts it in a flat list.
func (p Page) routeTree() []routeRow {
	children := map[string][]models.Route{}
	var roots []models.Route
	known := map[string]bool{}
	for _, route := range p.Routes {
		known[route.ID] = true
	}
	for _, route := range p.Routes {
		if route.ParentID != "" && known[route.ParentID] {
			children[route.ParentID] = append(children[route.ParentID], route)
			continue
		}
		roots = append(roots, route)
	}

	var rows []routeRow
	var walk func(route models.Route, depth int, destination, template inheritedRef)
	walk = func(route models.Route, depth int, destination, template inheritedRef) {
		if route.DestinationID != "" {
			destination = inheritedRef{ID: route.DestinationID}
		} else {
			destination.Inherited = destination.ID != ""
		}
		if route.TemplateID != "" {
			template = inheritedRef{ID: route.TemplateID}
		} else {
			template.Inherited = template.ID != ""
		}

		rows = append(rows, routeRow{Route: route, Depth: depth, Destination: destination, Template: template})
		for _, child := range children[route.ID] {
			walk(child, depth+1, destination, template)
		}
	}
	for _, root := range roots {
		walk(root, 0, inheritedRef{}, inheritedRef{})
	}
	return rows
}

// Depth as a left margin. The classes are literal so that Tailwind can see them.
func indent(depth int) string {
	switch depth {
	case 0:
		return ""
	case 1:
		return "ml-6"
	case 2:
		return "ml-12"
	case 3:
		return "ml-16"
	case 4:
		return "ml-20"
	default:
		return "ml-24"
	}
}

// parentOptions offers every route a new one could refine. A route cannot refine
// itself, and the empty option is what makes it a root.
func parentOptions(ctx context.Context, p Page) []option {
	out := make([]option, 0, len(p.Routes)+1)
	out = append(out, option{Value: "", Label: i18n.T(ctx, "routes.parent_none")})
	for _, route := range p.Routes {
		if p.EditRoute != nil && route.ID == p.EditRoute.ID {
			continue
		}
		out = append(out, option{Value: route.ID, Label: route.Name})
	}
	return out
}

// grantScope reads a grant back the way it was written: a Team, or one channel
// of it.
func grantScope(ctx context.Context, grant models.Grant) string {
	if grant.ChannelID == "" {
		return i18n.T(ctx, "grants.scope_team", grant.TeamID)
	}
	return i18n.T(ctx, "grants.scope_channel", grant.TeamID, grant.ChannelID)
}

// templateShape says what a template sends, which matters more in a list than
// how many bytes of card JSON it holds.
func templateShape(ctx context.Context, t models.Template) string {
	parts := []string{}
	if t.Title != "" {
		parts = append(parts, "title")
	}
	if t.Text != "" {
		parts = append(parts, "text")
	}
	if t.Body != "" {
		parts = append(parts, "card")
	}
	if len(parts) == 0 {
		return i18n.T(ctx, "templates.shape_empty")
	}
	return strings.Join(parts, " + ")
}

// The accessors below let a template read a field of the record under edit
// without repeating a nil check per field.

func (p Page) editingTemplate() models.Template {
	if p.EditTemplate == nil {
		return models.Template{}
	}
	return *p.EditTemplate
}

func (p Page) editingDestination() models.Destination {
	if p.EditDestination == nil {
		return models.Destination{}
	}
	return *p.EditDestination
}

func (p Page) editingRoute() models.Route {
	if p.EditRoute == nil {
		return models.Route{Priority: 100}
	}
	return *p.EditRoute
}

// selectorJSON renders a selector back into the JSON the form accepts. A new
// route gets a worked example rather than an empty box.
func selectorJSON(p Page) string {
	if p.EditRoute == nil {
		return `{"severity":"critical"}`
	}
	if len(p.EditRoute.LabelSelector) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(p.EditRoute.LabelSelector)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// submitLabel builds "Save a template" from two catalog entries rather than by
// concatenation: the noun does not always follow the verb, and in German it
// does not.
func submitLabel(ctx context.Context, editing bool, nounKey string) string {
	noun := i18n.T(ctx, nounKey)
	if editing {
		return i18n.T(ctx, "action.update", noun)
	}
	return i18n.T(ctx, "action.save", noun)
}

// destinationName resolves a route's destination to something readable, so the
// list shows names rather than the identifiers the route stores.
func (p Page) destinationName(id string) string {
	for _, d := range p.Destinations {
		if d.ID == id {
			return d.Name
		}
	}
	return id
}

func (p Page) templateName(id string) string {
	for _, t := range p.Templates {
		if t.ID == id {
			return t.Name
		}
	}
	return id
}

// option is a single choice in a select element.
type option struct {
	Value string
	Label string
}

func destinationOptions(p Page) []option {
	out := make([]option, 0, len(p.Destinations))
	for _, d := range p.Destinations {
		out = append(out, option{Value: d.ID, Label: d.Name})
	}
	return out
}

func templateOptions(p Page) []option {
	out := make([]option, 0, len(p.Templates))
	for _, t := range p.Templates {
		out = append(out, option{Value: t.ID, Label: t.Name})
	}
	return out
}

// selectorText renders a label selector the way an operator writes it.
func selectorText(ctx context.Context, selector map[string]string) string {
	if len(selector) == 0 {
		return i18n.T(ctx, "routes.selector_none")
	}

	keys := make([]string, 0, len(selector))
	for key := range selector {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+selector[key])
	}
	return strings.Join(pairs, ", ")
}

// A LanguageChoice is what the picker in the header needs: the languages this
// build carries, which one is showing, whether that was chosen or merely
// negotiated, and where to come back to.
type LanguageChoice struct {
	Languages []string
	Current   string
	Chosen    bool
	Return    string
}

type languageContextKey struct{}

// WithLanguageChoice carries it in the context, because Layout renders on every
// page and threading it through each one's parameters would touch them all.
func WithLanguageChoice(ctx context.Context, choice LanguageChoice) context.Context {
	return context.WithValue(ctx, languageContextKey{}, choice)
}

func languageOf(ctx context.Context) LanguageChoice {
	choice, _ := ctx.Value(languageContextKey{}).(LanguageChoice)
	return choice
}
