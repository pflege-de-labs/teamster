package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// DefaultTitle reproduces the summary line this service sent before templates
// could name their own, so an existing card-only template keeps its behaviour.
const DefaultTitle = `{{ default .Alert.Annotations.summary (default .Alert.Labels.alertname "Alert update") }}`

type RenderData struct {
	Alert any
	Now   string
}

// Message is a template rendered against an alert. Title is the line the Teams
// activity feed previews — a message that is only a card previews as "Card" —
// and Card is nil for a template that sends text alone.
type Message struct {
	Title string          `json:"title,omitempty"`
	Text  string          `json:"text,omitempty"`
	Card  json.RawMessage `json:"card,omitempty"`
}

// RenderMessage renders the three parts of a template. The text is sanitized
// here rather than at the Graph client, so every caller — delivery, preview and
// any future one — gets the same guarantee.
func RenderMessage(t models.Template, data RenderData) (Message, error) {
	var msg Message

	// A card with no title in front of it is what previews as "Card", so one is
	// supplied. Text needs no such help: the feed previews the text itself.
	title := t.Title
	if title == "" && t.Body != "" {
		title = DefaultTitle
	}
	if title != "" {
		rendered, err := renderText(title, data)
		if err != nil {
			return Message{}, fmt.Errorf("title: %w", err)
		}
		// The feed shows one line, so a template that spans several becomes one.
		msg.Title = strings.Join(strings.Fields(rendered), " ")
	}

	if t.Text != "" {
		text, err := renderText(t.Text, data)
		if err != nil {
			return Message{}, fmt.Errorf("text: %w", err)
		}
		safe, err := Sanitize(text)
		if err != nil {
			return Message{}, fmt.Errorf("text: %w", err)
		}
		msg.Text = safe
	}

	if t.Body != "" {
		card, err := Render(t.Body, data)
		if err != nil {
			return Message{}, err
		}
		msg.Card = card
	}

	return msg, nil
}

// Validate rejects a template that would render to nothing a reader can see.
func Validate(t models.Template) error {
	if t.Title == "" && t.Text == "" && t.Body == "" {
		return fmt.Errorf("a template needs a title, text or a card")
	}
	return nil
}

func renderText(body string, data RenderData) (string, error) {
	tmpl, err := template.New("text").Funcs(funcs()).Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

func Render(body string, data RenderData) (json.RawMessage, error) {
	tmpl, err := template.New("card").Funcs(funcs()).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	output := buf.Bytes()
	var tmp any
	if err := json.Unmarshal(output, &tmp); err != nil {
		return nil, fmt.Errorf("template output is not valid JSON: %w", err)
	}

	return json.RawMessage(output), nil
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"toJSON": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		// value is any because a missing key of a nil map arrives as an invalid
		// value, which a string parameter rejects outright — exactly the case a
		// fallback exists for.
		"default": func(value any, fallback string) string {
			text, ok := value.(string)
			if !ok || text == "" {
				return fallback
			}
			return text
		},
	}
}
