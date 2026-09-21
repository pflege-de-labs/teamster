package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
)

const maxMatchBytes = 1 << 16

type graphNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	Selector string `json:"selector,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Greedy   bool   `json:"greedy,omitempty"`
	Missing  bool   `json:"missing,omitempty"`

	// A route renders with a template, but a template is not somewhere an alert
	// goes, so it is a label on the route rather than a node of its own.
	Template          string `json:"template,omitempty"`
	TemplateInherited bool   `json:"template_inherited,omitempty"`
	TemplateMissing   bool   `json:"template_missing,omitempty"`

	X int `json:"x"`
	Y int `json:"y"`
}

// Alerts flow left to right: what arrives, what decides, where it lands. The
// layout is computed here so it is deterministic and the tests can see it,
// leaving the browser to draw and to handle dragging.
const (
	columnGap = 300
	rowGap    = 104
	groupGap  = 64
)

// Every edge means "an alert can go this way". The kind says which step it is,
// so the drawing can say whether a child delivers as well as its parent or
// instead of it.
const (
	linkEnters   = "enters"
	linkRefines  = "refines"
	linkDelivers = "delivers"
)

type graphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Greedy bool   `json:"greedy,omitempty"`
}

func (s *Server) handleRoutingGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.routingConfiguration(ctx, w)
	if err != nil {
		return
	}

	nodes, links := buildGraph(cfg.routes, cfg.destinations, cfg.recipients, cfg.templates, s.channelNamer(cfg.destinations))
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "links": links})
}

// handleTemplateGraph answers the second, smaller picture: which routes render
// with which template. It is a separate graph because "renders with" is not a
// step an alert takes, and drawing it over the flow made one arrow mean two
// things.
func (s *Server) handleTemplateGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.routingConfiguration(ctx, w)
	if err != nil {
		return
	}

	nodes, links := buildTemplateGraph(cfg.routes, cfg.templates)
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "links": links})
}

// routingConfiguration reads what both pictures are drawn from, reporting the
// failure itself so each handler stays about its own graph.
type routingConfig struct {
	routes       []models.Route
	destinations []models.Destination
	recipients   []models.Recipient
	templates    []models.Template
}

func (s *Server) routingConfiguration(ctx context.Context, w http.ResponseWriter) (routingConfig, error) {
	var cfg routingConfig

	routes, err := s.store.ListRoutes(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return cfg, err
	}
	destinations, err := s.store.ListDestinations(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return cfg, err
	}
	recipients, err := s.store.ListRecipients(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return cfg, err
	}
	templates, err := s.store.ListTemplates(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return cfg, err
	}
	cfg = routingConfig{routes: routes, destinations: destinations, recipients: recipients, templates: templates}
	return cfg, nil
}

// buildGraph draws the path an alert can take: the webhook, the routes in
// evaluation order with children hanging off their parents, and the channels
// they deliver to. A route pointing at something deleted becomes a node marked
// missing rather than a dropped link, because that broken state is exactly what
// the view exists to show.
func buildGraph(routes []models.Route, destinations []models.Destination, recipients []models.Recipient, templates []models.Template, channelName func(teamID, channelID string) string) ([]graphNode, []graphLink) {
	nodes := []graphNode{}
	links := []graphLink{}
	index := map[string]bool{}

	add := func(node graphNode) {
		if !index[node.ID] {
			index[node.ID] = true
			nodes = append(nodes, node)
		}
	}

	// Every alert enters the same router, so the flow starts from one node
	// rather than from each webhook endpoint.
	const sourceID = "source:webhook"
	add(graphNode{
		ID:     sourceID,
		Kind:   "source",
		Label:  "Incoming alerts",
		Detail: "POST /webhook/alertmanager · /webhook/universal",
	})

	ordered := evaluationOrder(routes)
	known := map[string]bool{}
	for _, route := range ordered {
		known[route.ID] = true
	}

	templateNames := map[string]string{}
	for _, template := range templates {
		templateNames[template.ID] = template.Name
	}

	// A child sits one column right of its parent, so depth in the tree reads as
	// distance from the webhook.
	depths := routeDepths(ordered)
	effective := inheritedTargets(ordered)
	rows := map[int]int{}
	maxDepth := 0

	for _, route := range ordered {
		depth := depths[route.ID]
		if depth > maxDepth {
			maxDepth = depth
		}

		targets := effective[route.ID]
		node := graphNode{
			ID:       "route:" + route.ID,
			Kind:     "route",
			Label:    route.Name,
			Selector: selectorSummary(route.LabelSelector),
			Priority: route.Priority,
			Default:  route.IsDefault,
			Greedy:   route.Greedy,
			X:        (1 + depth) * columnGap,
			Y:        rows[depth] * rowGap,
		}
		if targets.templateID != "" {
			node.TemplateInherited = route.TemplateID == ""
			if name, ok := templateNames[targets.templateID]; ok {
				node.Template = name
			} else {
				node.Template = targets.templateID
				node.TemplateMissing = true
			}
		}
		add(node)
		rows[depth]++

		// A child hangs off its parent, because that is the order it is
		// evaluated in; only a root is reached straight from the webhook.
		if route.ParentID != "" && known[route.ParentID] {
			links = append(links, graphLink{
				Source: "route:" + route.ParentID,
				Target: "route:" + route.ID,
				Kind:   linkRefines,
				Greedy: route.Greedy,
			})
			continue
		}
		links = append(links, graphLink{Source: sourceID, Target: "route:" + route.ID, Kind: linkEnters})
	}

	sinkColumn := (2 + maxDepth) * columnGap
	row := 0
	for _, destination := range destinations {
		add(graphNode{
			ID:     "destination:" + destination.ID,
			Kind:   "destination",
			Label:  destination.Name,
			Detail: channelName(destination.TeamID, destination.ChannelID),
			X:      sinkColumn,
			Y:      row * rowGap,
		})
		row++
	}

	// A person is a sink like a channel is, and a separate kind because it is
	// reached through a different transport and drawn differently.
	for _, recipient := range recipients {
		add(graphNode{
			ID:     "recipient:" + recipient.ID,
			Kind:   "recipient",
			Label:  recipientLabel(recipient),
			Detail: recipient.Subject,
			X:      sinkColumn,
			Y:      row * rowGap,
		})
		row++
	}

	missingTop := row*rowGap + groupGap
	missing := 0
	// One route can now deliver twice, so both targets are walked and each
	// draws its own link -- the same fan-out routing.collect produces.
	for _, route := range ordered {
		targets := effective[route.ID]
		for _, target := range []struct{ prefix, id, label string }{
			{"destination:", targets.destinationID, "missing destination"},
			{"recipient:", targets.recipientID, "missing recipient"},
		} {
			if target.id == "" {
				continue
			}
			nodeID := target.prefix + target.id
			if !index[nodeID] {
				add(graphNode{
					ID:      nodeID,
					Kind:    strings.TrimSuffix(target.prefix, ":"),
					Label:   target.label,
					Detail:  target.id,
					Missing: true,
					X:       sinkColumn,
					Y:       missingTop + missing*rowGap,
				})
				missing++
			}
			links = append(links, graphLink{Source: "route:" + route.ID, Target: nodeID, Kind: linkDelivers})
		}
	}

	return nodes, links
}

// recipientLabel is what a person is called in the picture. Name is what an
// operator recognises, but it comes from Teams and nothing guarantees it is
// set, so the subject that identifies them is the fallback.
func recipientLabel(recipient models.Recipient) string {
	if recipient.Name != "" {
		return recipient.Name
	}
	return recipient.Subject
}

// buildTemplateGraph pairs each template with the routes that render with it.
// A template no route references has no edges, which is how an orphan shows up
// now that templates are not nodes in the flow.
func buildTemplateGraph(routes []models.Route, templates []models.Template) ([]graphNode, []graphLink) {
	nodes := []graphNode{}
	links := []graphLink{}
	index := map[string]bool{}

	add := func(node graphNode) {
		if !index[node.ID] {
			index[node.ID] = true
			nodes = append(nodes, node)
		}
	}

	ordered := evaluationOrder(routes)
	effective := inheritedTargets(ordered)

	users := map[string][]models.Route{}
	for _, route := range ordered {
		if target := effective[route.ID].templateID; target != "" {
			users[target] = append(users[target], route)
		}
	}

	known := map[string]bool{}
	for _, template := range templates {
		known[template.ID] = true
	}

	row := 0
	drawTemplate := func(id, label string, isMissing bool) {
		add(graphNode{
			ID:      "template:" + id,
			Kind:    "template",
			Label:   label,
			Missing: isMissing,
			X:       0,
			Y:       row * rowGap,
		})

		for _, route := range users[id] {
			add(graphNode{
				ID:       "route:" + route.ID,
				Kind:     "route",
				Label:    route.Name,
				Selector: selectorSummary(route.LabelSelector),
				Priority: route.Priority,
				Default:  route.IsDefault,
				X:        columnGap,
				Y:        row * rowGap,
			})
			links = append(links, graphLink{Source: "template:" + id, Target: "route:" + route.ID, Kind: "renders"})
			row++
		}
		if len(users[id]) == 0 {
			row++
		}
	}

	for _, template := range templates {
		detail := "no route renders with it"
		if len(users[template.ID]) > 0 {
			detail = ""
		}
		drawTemplate(template.ID, template.Name, false)
		if detail != "" {
			for i := range nodes {
				if nodes[i].ID == "template:"+template.ID {
					nodes[i].Detail = detail
				}
			}
		}
	}

	// A route rendering with a template that was deleted belongs here too: the
	// flow graph shows it delivering, and this one shows what it renders with.
	for id := range users {
		if !known[id] {
			drawTemplate(id, "missing template", true)
		}
	}

	return nodes, links
}

// evaluationOrder is the order the router reads routes in, so both pictures list
// them the way they are tried: highest priority first, the default last.
func evaluationOrder(routes []models.Route) []models.Route {
	ordered := append([]models.Route(nil), routes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].IsDefault != ordered[j].IsDefault {
			return !ordered[i].IsDefault
		}
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority > ordered[j].Priority
		}
		return ordered[i].Name < ordered[j].Name
	})
	return ordered
}

// channelNamer resolves the ids a destination stores into the names an
// operator recognises. Graph is best effort here: the picture is still worth
// drawing when the directory is unreachable, so a failed lookup falls back to
// the raw ids rather than failing the request.
func (s *Server) channelNamer(destinations []models.Destination) func(teamID, channelID string) string {
	teamNames := map[string]string{}
	if teams, err := s.directory.Teams(s.graph.ListTeams); err == nil {
		for _, team := range teams {
			teamNames[team.ID] = team.Name
		}
	}

	channelNames := map[string]string{}
	fetched := map[string]bool{}
	for _, destination := range destinations {
		if destination.TeamID == "" || fetched[destination.TeamID] {
			continue
		}
		fetched[destination.TeamID] = true

		channels, err := s.directory.Channels(destination.TeamID, func() ([]graph.Channel, error) {
			return s.graph.ListChannels(destination.TeamID)
		})
		if err != nil {
			continue
		}
		for _, channel := range channels {
			channelNames[destination.TeamID+"/"+channel.ID] = channel.Name
		}
	}

	return func(teamID, channelID string) string {
		team := teamNames[teamID]
		if team == "" {
			team = teamID
		}
		channel := channelNames[teamID+"/"+channelID]
		if channel == "" {
			channel = channelID
		}
		return team + " › " + channel
	}
}

// routeDepths counts how far each route sits below a root, stopping at MaxDepth
// so a tree broken by a cycle still produces a drawing.
func routeDepths(routes []models.Route) map[string]int {
	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	depths := map[string]int{}
	for _, route := range routes {
		depth := 0
		for parent, ok := byID[route.ParentID]; ok && depth < routing.MaxDepth; parent, ok = byID[parent.ParentID] {
			depth++
		}
		depths[route.ID] = depth
	}
	return depths
}

type routeTargets struct {
	destinationID string
	recipientID   string
	templateID    string
}

// inheritedTargets resolves every route's destination, recipient and template
// through its ancestors, stopping at a parent that is missing or at MaxDepth so
// a broken tree cannot loop here.
//
// This is the second implementation of the walk routing.collect does, because
// the picture is drawn from the whole tree rather than from one alert's path
// through it. The two have to agree: a graph that disagrees with routing is a
// picture of a system nobody is running.
func inheritedTargets(routes []models.Route) map[string]routeTargets {
	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	effective := map[string]routeTargets{}
	for _, route := range routes {
		targets := routeTargets{
			destinationID: route.DestinationID,
			recipientID:   route.RecipientID,
			templateID:    route.TemplateID,
		}
		parent, ok := byID[route.ParentID]
		for depth := 0; ok && depth < routing.MaxDepth; depth++ {
			if targets.destinationID == "" {
				targets.destinationID = parent.DestinationID
			}
			if targets.recipientID == "" {
				targets.recipientID = parent.RecipientID
			}
			if targets.templateID == "" {
				targets.templateID = parent.TemplateID
			}
			if targets.destinationID != "" && targets.recipientID != "" && targets.templateID != "" {
				break
			}
			parent, ok = byID[parent.ParentID]
		}
		effective[route.ID] = targets
	}
	return effective
}

// selectorSummary renders a selector the way an operator writes it. Unlike the
// views version it leaves an empty selector empty, because a graph node shows
// nothing rather than prose about matching nothing.
func selectorSummary(selector map[string]string) string {
	if len(selector) == 0 {
		return ""
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

func (s *Server) handleRoutingMatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMatchBytes)

	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	result, err := s.router.Plan(ctx, req.Labels)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	routes, err := s.store.ListRoutes(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	answer := map[string]any{
		"reason":      result.Reason,
		"explanation": explainResult(result, byID),
		"deliveries":  deliveryAnswers(result, byID),
		"nodes":       matchedNodes(result),
	}
	// Every root whose selector matched, not just the first — nothing says two
	// independent routes cannot both name `severity=critical`, and both fire.
	if len(result.Roots) > 0 {
		routes := make([]map[string]any, 0, len(result.Roots))
		for _, id := range result.Roots {
			if root, ok := byID[id]; ok {
				routes = append(routes, routeAnswer(root))
			}
		}
		answer["routes"] = routes
	}
	writeJSON(w, http.StatusOK, answer)
}

func routeAnswer(route models.Route) map[string]any {
	return map[string]any{
		"id":       route.ID,
		"name":     route.Name,
		"node":     "route:" + route.ID,
		"selector": selectorSummary(route.LabelSelector),
		"priority": route.Priority,
	}
}

func deliveryAnswers(result routing.Result, byID map[string]models.Route) []map[string]any {
	answers := make([]map[string]any, 0, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		answer := map[string]any{
			"route":       routeAnswer(byID[delivery.RouteID]),
			"kind":        delivery.Kind,
			"template_id": delivery.TemplateID,
			"reason":      delivery.Reason,
		}
		// Only the one its kind names: a chat delivery has no destination, and
		// reporting an empty one reads as "delivers nowhere".
		if delivery.DestinationID != "" {
			answer["destination_id"] = delivery.DestinationID
		}
		if delivery.RecipientID != "" {
			answer["recipient_id"] = delivery.RecipientID
		}
		answers = append(answers, answer)
	}
	return answers
}

// Every node on the path an alert takes, so the picture can highlight the whole
// fan-out rather than one route of it.
func matchedNodes(result routing.Result) []string {
	nodes := make([]string, 0, len(result.Deliveries)*2)
	for _, delivery := range result.Deliveries {
		nodes = append(nodes, "route:"+delivery.RouteID)
		if delivery.DestinationID != "" {
			nodes = append(nodes, "destination:"+delivery.DestinationID)
		}
		if delivery.RecipientID != "" {
			nodes = append(nodes, "recipient:"+delivery.RecipientID)
		}
	}
	return nodes
}

// explainResult is the human-readable half of a match: the machine-readable
// half is "reason" plus "deliveries", each carrying its own reason already.
// One matched root is by far the common case, so it keeps the exact wording
// this had before more than one root could match at all; two or more get
// their own paragraph rather than picking one to feature over the rest, since
// none of them is more the answer than another.
func explainResult(result routing.Result, byID map[string]models.Route) string {
	switch result.Reason {
	case routing.ReasonNoRoutes:
		return "no routes are configured, so this alert would be rejected"
	case routing.ReasonNone:
		return "no selector matched and no default route exists, so this alert would be rejected"
	}

	names := make([]string, 0, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		names = append(names, fmt.Sprintf("%q", delivery.RouteName))
	}

	if len(result.Roots) == 1 {
		root := byID[result.Roots[0]]
		opening := fmt.Sprintf("%q matched on %s, at priority %d", root.Name, selectorSummary(root.LabelSelector), root.Priority)
		if result.Reason == routing.ReasonDefault {
			opening = fmt.Sprintf("no selector matched, so the default route %q takes it", root.Name)
		}

		rootDelivers := false
		for _, delivery := range result.Deliveries {
			if delivery.RouteID == result.Roots[0] {
				rootDelivers = true
				break
			}
		}
		switch {
		case len(result.Deliveries) == 1 && rootDelivers:
			return opening
		case rootDelivers:
			return fmt.Sprintf("%s, and %d messages go out — from %s", opening, len(result.Deliveries), strings.Join(names, ", "))
		default:
			return fmt.Sprintf("%s, but a child route delivers instead of it — from %s", opening, strings.Join(names, ", "))
		}
	}

	rootNames := make([]string, 0, len(result.Roots))
	for _, id := range result.Roots {
		rootNames = append(rootNames, fmt.Sprintf("%q", byID[id].Name))
	}
	return fmt.Sprintf("%d routes matched — %s — and %d messages go out: %s",
		len(result.Roots), strings.Join(rootNames, ", "), len(result.Deliveries), strings.Join(names, ", "))
}
