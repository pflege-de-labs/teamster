// Package views renders the admin UI. The templ sources next to this file are
// compiled to *_templ.go by `make generate`, and the generated files are
// committed so a plain `go build` needs no extra tooling.
package views

//go:generate go tool templ generate
//go:generate ../../../bin/tailwindcss --input styles.css --output ../web/styles.css --minify

import (
	"sort"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// Page carries everything the admin page renders. The lists come straight from
// the store, and Notice reports the outcome of the last form submission.
type Page struct {
	Templates    []models.Template
	Destinations []models.Destination
	Routes       []models.Route
	Notice       string
	Error        string
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
func selectorText(selector map[string]string) string {
	if len(selector) == 0 {
		return "matches nothing on its own"
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
