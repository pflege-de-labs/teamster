// Package views renders the admin UI. The templ sources next to this file are
// compiled to *_templ.go by `make generate`, and the generated files are
// committed so a plain `go build` needs no extra tooling.
package views

//go:generate go tool templ generate
//go:generate ../../../bin/tailwindcss --input styles.css --output ../web/styles.css --minify

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/cards"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
	render "github.com/pflege-de-labs/teamster/internal/templates"
)

// A Viewer is who is looking at a page: the name the provider gave them and the
// roles they hold. The header shows both, so a missing control has a visible
// reason.
type Viewer struct {
	Name  string
	Roles []string
	// CanManage decides whether the permissions tab is offered. The page
	// refuses the request either way; this is what keeps it out of the nav.
	CanManage bool
	// CanAudit decides whether the nav offers the audit trail.
	CanAudit bool
	// NotificationsEnabled decides whether the nav offers the notifications
	// page. Unlike CanManage this is not a per-viewer permission -- the page
	// itself is offered to every role -- but a deployment-wide switch: the
	// route behind the link exists only when the bot is configured, so
	// linking to it otherwise would send someone to mint a code for a bot
	// that was never registered to receive it.
	NotificationsEnabled bool
	// CanComplete is whether the editors may fetch /api/samples; without it
	// they still complete template syntax, but no label keys or values.
	CanComplete bool
	// PeopleEnabled is whether the app is installed for everyone, which is
	// when /admin/people exists; the nav shows it to admins only.
	PeopleEnabled bool
}

// roleLabel names the highest built-in role, translated, and says so plainly
// when there is none — that is the case where the page is empty and the reason
// needs to be in front of them. Other claim values are on the user info page.
func (v Viewer) roleLabel(ctx context.Context) string {
	roles := make([]authz.Role, 0, len(v.Roles))
	for _, role := range v.Roles {
		roles = append(roles, authz.Role(role))
	}
	highest := authz.Highest(roles)
	if highest == authz.RoleNone {
		return i18n.T(ctx, "header.no_role")
	}
	return roleName(ctx, string(highest))
}

// roleName translates a built-in role and leaves one a deployment defined as
// it is spelled.
func roleName(ctx context.Context, role string) string {
	if !authz.Valid(authz.Role(role)) {
		return role
	}
	return i18n.T(ctx, "role."+role)
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
	// InstallStates is each destination team's state for the bot's app.
	InstallStates map[string]string
	Recipients    []models.Recipient
	// CanAddress offers the target that delivers to the people a message
	// names (ADR 0062).
	CanAddress       bool
	Routes           []models.Route
	WebhookEndpoints []models.WebhookEndpoint
	// GlobalDefault is where a message no route claims goes; nil when there
	// is no destination at all.
	GlobalDefault *models.Destination
	// GlobalDefaultTemplateID is what the catch-all renders with; "" is the
	// built-in default message (ADR 0050).
	GlobalDefaultTemplateID string
	// SourceDefaults maps a webhook source to its default template (ADR 0055).
	SourceDefaults map[string]string
	Notice         string
	Error          string

	// Sample events the preview can render the template against.
	PreviewSamples []string

	// Snippets and Starter are what the card palette inserts.
	Snippets []cards.Snippet
	Starter  string
	// Presets are the whole templates a new one can start from.
	Presets []cards.Preset

	// Vocabulary is what the template editor completes inside {{ }}.
	Vocabulary render.Vocabulary

	// CanEdit hides what this session may not do. Hiding is not enforcing —
	// the server refuses the post either way — but showing a viewer a Save
	// button they cannot use is its own kind of broken.
	CanEdit bool

	// Viewer is who the page is being rendered for, shown in the header.
	Viewer Viewer

	// CanManage decides whether the way to the permissions page is offered.
	CanManage bool

	// BrokerAvailable decides whether the destination picker offers "my
	// Teams" beside the tenant-wide list: the delegated-Teams feature (ADR
	// 0037) must be configured on, and this session must be the kind with a
	// Keycloak login behind it for there to be a broker token to ask for.
	BrokerAvailable bool

	// A nil Edit* means the matching form creates rather than updates.
	EditTemplate        *models.Template
	EditDestination     *models.Destination
	EditRoute           *models.Route
	EditWebhookEndpoint *models.WebhookEndpoint

	// NewWebhookURL is a secret this page is the only chance to read: only a
	// digest of the token is stored, so nothing can show it again. It is shown
	// in the response to the post that generated it rather than carried through
	// a redirect, because a redirect would put the secret in a query string,
	// and from there into the browser history and this server's own access log.
	NewWebhookURL string

	// Tab is the section asked for; activeTab settles what is shown.
	Tab string
}

// tabs are the sections of /admin, in the order the tab bar shows them.
func (p Page) tabs() []string {
	tabs := []string{"templates", "destinations", "webhooks", "routes"}
	if p.CanManage {
		tabs = append(tabs, "grants")
	}
	return tabs
}

// activeTab is Tab when this viewer has that tab, else the first one.
func (p Page) activeTab() string {
	if slices.Contains(p.tabs(), p.Tab) {
		return p.Tab
	}
	return p.tabs()[0]
}

// A routeRow is a route as the list draws it: its place in the tree, and where
// its destination and template came from.
type routeRow struct {
	Route       models.Route
	Depth       int
	Destination inheritedRef
	Template    inheritedRef
	// Addressed is set, with ID "addressed", when the route delivers to the
	// people a message names, its own choice or inherited (ADR 0062).
	Addressed inheritedRef
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
	var walk func(route models.Route, depth int, destination, template, addressed inheritedRef)
	walk = func(route models.Route, depth int, destination, template, addressed inheritedRef) {
		switch {
		case route.Addressed:
			destination, addressed = inheritedRef{}, inheritedRef{ID: "addressed"}
		case route.DestinationID != "":
			destination, addressed = inheritedRef{ID: route.DestinationID}, inheritedRef{}
		case route.RecipientID != "":
			addressed = inheritedRef{}
			destination.Inherited = destination.ID != ""
		default:
			destination.Inherited = destination.ID != ""
			addressed.Inherited = addressed.ID != ""
		}
		if route.TemplateID != "" {
			template = inheritedRef{ID: route.TemplateID}
		} else {
			template.Inherited = template.ID != ""
		}

		rows = append(rows, routeRow{Route: route, Depth: depth, Destination: destination, Template: template, Addressed: addressed})
		for _, child := range children[route.ID] {
			walk(child, depth+1, destination, template, addressed)
		}
	}
	for _, root := range roots {
		walk(root, 0, inheritedRef{}, inheritedRef{}, inheritedRef{})
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

func (p Page) editingWebhookEndpoint() models.WebhookEndpoint {
	if p.EditWebhookEndpoint == nil {
		return models.WebhookEndpoint{}
	}
	return *p.EditWebhookEndpoint
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
	// Sources is a template's comma-separated webhooks, for the script that
	// narrows a picker to what a selector can receive; empty means any.
	Sources string
}

func destinationOptions(p Page) []option {
	out := make([]option, 0, len(p.Destinations))
	for _, d := range p.Destinations {
		out = append(out, option{Value: d.ID, Label: d.Name})
	}
	return out
}

// routeTargetOptions is one list for both kinds of target, so a route names a
// channel or a person and never both (ADR 0047). The leading empty choice is a
// child inheriting its parent's target. Recipients are already narrowed to the
// chats this session may route to.
func routeTargetOptions(ctx context.Context, p Page) []option {
	out := make([]option, 0, len(p.Destinations)+len(p.Recipients)+1)
	out = append(out, option{Value: "", Label: i18n.T(ctx, "routes.target_none")})
	for _, d := range p.Destinations {
		out = append(out, option{Value: "destination:" + d.ID, Label: i18n.T(ctx, "routes.target_channel", d.Name)})
	}
	for _, r := range p.Recipients {
		label := r.Name
		if label == "" {
			label = r.Subject
		}
		out = append(out, option{Value: "recipient:" + r.ID, Label: i18n.T(ctx, "routes.target_person", label)})
	}
	// An edited route that already addresses people shows it, so saving does
	// not silently pick another target.
	if p.CanAddress || p.editingRoute().Addressed {
		out = append(out, option{Value: "addressed", Label: i18n.T(ctx, "routes.target_addressed")})
	}
	return out
}

// routeTargetValue is the option an edited route selects. A route saved
// before ADR 0047 with both targets shows its channel; saving it drops the
// person, which the list's badge warns about.
func routeTargetValue(route models.Route) string {
	switch {
	case route.Addressed:
		return "addressed"
	case route.DestinationID != "":
		return "destination:" + route.DestinationID
	case route.RecipientID != "":
		return "recipient:" + route.RecipientID
	}
	return ""
}

func templateOptions(p Page) []option {
	out := make([]option, 0, len(p.Templates))
	for _, t := range p.Templates {
		out = append(out, option{Value: t.ID, Label: t.Name, Sources: strings.Join(t.Sources, ",")})
	}
	return out
}

// sourceOptions are the template form's source checkboxes (ADR 0053).
func sourceOptions(ctx context.Context) []option {
	out := make([]option, 0, len(models.AllSources()))
	for _, source := range models.AllSources() {
		out = append(out, option{Value: source, Label: i18n.T(ctx, "source."+source)})
	}
	return out
}

// sourcesText is a template's sources as the list shows them.
func sourcesText(ctx context.Context, t models.Template) string {
	if len(t.Sources) == 0 {
		return i18n.T(ctx, "templates.sources_any")
	}
	names := make([]string, 0, len(t.Sources))
	for _, source := range t.Sources {
		names = append(names, i18n.T(ctx, "source."+source))
	}
	return strings.Join(names, ", ")
}

// routeTemplateOptions is templateOptions with a leading "none" choice, so a
// route can be saved without one -- a select always submits some value, so
// without this entry the form could never actually express "no template,
// send the payload's own title/text/card directly" (ADR 0036).
func routeTemplateOptions(ctx context.Context, p Page) []option {
	out := make([]option, 0, len(p.Templates)+1)
	out = append(out, option{Value: "", Label: i18n.T(ctx, "routes.template_none")})
	return append(out, templateOptions(p)...)
}

// globalDefaultTemplateOptions leads with the built-in default message, which
// is what the catch-all sends when no template is chosen.
func globalDefaultTemplateOptions(ctx context.Context, p Page) []option {
	out := make([]option, 0, len(p.Templates)+1)
	out = append(out, option{Value: "", Label: i18n.T(ctx, "routes.global_default_builtin")})
	return append(out, templateOptions(p)...)
}

// sourceDefaultOptions offers the templates that handle source, after the
// built-in message a source without a default sends.
func sourceDefaultOptions(ctx context.Context, p Page, source string) []option {
	out := []option{{Value: "", Label: i18n.T(ctx, "templates.source_default_builtin")}}
	for _, t := range p.Templates {
		if t.Handles(source) {
			out = append(out, option{Value: t.ID, Label: t.Name})
		}
	}
	return out
}

// defaultFor lists the sources a template is the default of, for the list's badge.
func (p Page) defaultFor(ctx context.Context, id string) string {
	var names []string
	for _, source := range models.AllSources() {
		if p.SourceDefaults[source] == id {
			names = append(names, i18n.T(ctx, "source."+source))
		}
	}
	return strings.Join(names, ", ")
}

// presetJSON is a preset as the editor script reads it.
func presetJSON(p cards.Preset) string {
	b, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// webhookTemplateOptions offers only the templates that handle a Teams V2
// payload, which is all an endpoint receives.
func webhookTemplateOptions(ctx context.Context, p Page) []option {
	out := make([]option, 0, len(p.Templates)+1)
	out = append(out, option{Value: "", Label: i18n.T(ctx, "webhooks.template_none")})
	for _, t := range p.Templates {
		if t.Handles(models.SourceTeamsV2) {
			out = append(out, option{Value: t.ID, Label: t.Name})
		}
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

// flagFor is the flag the user menu shows for a shipped language, or "" for one
// a locale directory added.
func flagFor(tag string) string {
	switch tag {
	case "de":
		return "/flags/de.svg"
	case "en":
		return "/flags/gb.svg"
	}
	return ""
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

// TimeZoneChoice is the zone every time on a page is shown in.
type TimeZoneChoice struct {
	Location *time.Location
	// Return is the page to come back to after switching.
	Return string
}

func (c TimeZoneChoice) utc() bool {
	return c.Location == nil || c.Location == time.UTC
}

type timeZoneContextKey struct{}

// WithTimeZone carries the zone in the context, for the same reason as
// WithLanguageChoice.
func WithTimeZone(ctx context.Context, choice TimeZoneChoice) context.Context {
	return context.WithValue(ctx, timeZoneContextKey{}, choice)
}

func timeZoneOf(ctx context.Context) TimeZoneChoice {
	choice, _ := ctx.Value(timeZoneContextKey{}).(TimeZoneChoice)
	return choice
}

// stamp shows t in the operator's zone, named, so a time is never read in the
// wrong one.
func stamp(ctx context.Context, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	loc := timeZoneOf(ctx).Location
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("2006-01-02 15:04 MST")
}

type versionContextKey struct{}

// WithVersion carries the build version for the sidebar footer, for the same
// reason as WithLanguageChoice.
func WithVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, versionContextKey{}, version)
}

func versionOf(ctx context.Context) string {
	version, _ := ctx.Value(versionContextKey{}).(string)
	return version
}

func roleNames(ctx context.Context, roles []string) []string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, roleName(ctx, role))
	}
	return names
}
