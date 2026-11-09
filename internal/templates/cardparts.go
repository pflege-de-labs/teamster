package templates

import (
	"encoding/json"
	"strings"
)

// CardParts is an Adaptive Card taken apart, so a template can lay out a
// sender's content its own way rather than pass the card through (ADR 0099).
//
// shortcut: grouped lists lose how texts and facts interleave; add an ordered
// list of blocks when a sender needs that order.
type CardParts struct {
	Texts   []string
	Facts   []Fact
	Actions []Action
}

// Fact is one row of a FactSet.
type Fact struct {
	Title string
	Value string
}

// Action is a link the card offered: an Action.OpenUrl.
type Action struct {
	Title string
	URL   string
}

// cardParts accepts a card as .Event.Card carries it, or as a map reached
// through .Payload. Anything else is no card: a text-only message has none,
// and a template that runs on it must still render.
func cardParts(card any) CardParts {
	var root map[string]any
	switch c := card.(type) {
	case map[string]any:
		root = c
	case json.RawMessage:
		_ = json.Unmarshal(c, &root)
	case []byte:
		_ = json.Unmarshal(c, &root)
	}

	var parts CardParts
	parts.walk(root["body"])
	parts.actions(root["actions"])
	return parts
}

// walk visits elements in document order, descending into every container
// kind Adaptive Cards has.
func (p *CardParts) walk(elements any) {
	list, _ := elements.([]any)
	for _, item := range list {
		el, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch el["type"] {
		case "TextBlock":
			p.text(str(el["text"]))
		case "RichTextBlock":
			p.text(inlines(el["inlines"]))
		case "FactSet":
			facts, _ := el["facts"].([]any)
			for _, f := range facts {
				if fact, ok := f.(map[string]any); ok {
					p.Facts = append(p.Facts, Fact{Title: str(fact["title"]), Value: str(fact["value"])})
				}
			}
		case "ActionSet":
			p.actions(el["actions"])
		}
		for _, key := range []string{"items", "columns", "rows", "cells"} {
			p.walk(el[key])
		}
	}
}

func (p *CardParts) text(s string) {
	if s = strings.TrimSpace(s); s != "" {
		p.Texts = append(p.Texts, s)
	}
}

func (p *CardParts) actions(actions any) {
	list, _ := actions.([]any)
	for _, item := range list {
		a, ok := item.(map[string]any)
		if ok && a["type"] == "Action.OpenUrl" && str(a["url"]) != "" {
			p.Actions = append(p.Actions, Action{Title: str(a["title"]), URL: str(a["url"])})
		}
	}
}

// inlines joins a RichTextBlock's runs, which are strings or TextRun objects.
func inlines(v any) string {
	list, _ := v.([]any)
	var b strings.Builder
	for _, item := range list {
		switch run := item.(type) {
		case string:
			b.WriteString(run)
		case map[string]any:
			b.WriteString(str(run["text"]))
		}
	}
	return b.String()
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
