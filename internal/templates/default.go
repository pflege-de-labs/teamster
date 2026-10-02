package templates

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// Default is the message a delivery sends when nothing says what it should
// look like: no template on the route, and no title, text or card in the
// payload. It is best effort -- a readable title, the state and description,
// and the whole event as JSON -- so a reader can still act on it.
func Default(ev models.Event) (Message, error) {
	attributes := models.AttributesOf(ev)
	title := attributes["summary"]
	if title == "" {
		title = ev.Labels["alertname"]
	}
	if title == "" {
		title = "Update"
	}

	var text strings.Builder
	if ev.State != models.StateNone {
		fmt.Fprintf(&text, "**State:** %s\n\n", ev.State)
	}
	if description := attributes["description"]; description != "" {
		text.WriteString(description + "\n\n")
	}
	payload, err := json.MarshalIndent(defaultPayload(ev), "", "  ")
	if err != nil {
		return Message{}, fmt.Errorf("payload: %w", err)
	}
	fence := codeFence(string(payload))
	text.WriteString(fence + "json\n" + string(payload) + "\n" + fence + "\n")

	safe, err := RenderText(text.String())
	if err != nil {
		return Message{}, fmt.Errorf("text: %w", err)
	}
	return Message{Title: strings.Join(strings.Fields(title), " "), Text: safe}, nil
}

// defaultPayload is the event as a sender would recognise it: the fields it
// set, without the ones only direct content uses, and without zero times.
func defaultPayload(ev models.Event) map[string]any {
	out := map[string]any{}
	add := func(key string, value any, present bool) {
		if present {
			out[key] = value
		}
	}
	addTime := func(key string, t time.Time) {
		add(key, t.Format(time.RFC3339), !t.IsZero())
	}
	add("source", ev.Source, ev.Source != "")
	add("key", ev.Key, ev.Key != "")
	add("state", ev.State, ev.State != models.StateNone)
	add("labels", ev.Labels, len(ev.Labels) > 0)
	if am := ev.Alertmanager; am != nil {
		add("annotations", am.Annotations, len(am.Annotations) > 0)
		addTime("starts_at", am.StartsAt)
		addTime("ends_at", am.EndsAt)
		add("generator_url", am.GeneratorURL, am.GeneratorURL != "")
		// One alert is already shown by the flat fields above.
		if len(am.Alerts) > 1 {
			add("common_annotations", am.CommonAnnotations, len(am.CommonAnnotations) > 0)
			alerts := make([]map[string]any, 0, len(am.Alerts))
			for _, alert := range am.Alerts {
				one := map[string]any{"status": alert.Status, "labels": alert.Labels}
				if len(alert.Annotations) > 0 {
					one["annotations"] = alert.Annotations
				}
				if !alert.StartsAt.IsZero() {
					one["starts_at"] = alert.StartsAt.Format(time.RFC3339)
				}
				alerts = append(alerts, one)
			}
			out["alerts"] = alerts
		}
	}
	if u := ev.Universal; u != nil {
		add("attributes", u.Attributes, len(u.Attributes) > 0)
		addTime("time", u.Time)
		add("url", u.URL, u.URL != "")
	}
	return out
}

// codeFence outlasts any backtick run in the code, so a label value cannot
// close the block early.
func codeFence(code string) string {
	longest, run := 0, 0
	for _, r := range code {
		if r != '`' {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	return strings.Repeat("`", max(3, longest+1))
}

// HintNoTemplate is what the hint card says. A message goes to a shared
// channel, not to one reader, so it is not localized.
const HintNoTemplate = "No template is defined for this message, so teamster sent its built-in default."

// HintCard is the card that follows a message sent without a template. It
// links to the admin UI when adminURL is known, and only says so otherwise.
func HintCard(adminURL, path string) (json.RawMessage, error) {
	text := HintNoTemplate
	if adminURL == "" {
		text += " Create one in the teamster admin UI."
	}
	card := map[string]any{
		"type":    "AdaptiveCard",
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"version": "1.4",
		"body": []any{map[string]any{
			"type": "TextBlock", "text": text, "wrap": true, "isSubtle": true, "size": "Small",
		}},
	}
	if adminURL != "" {
		card["actions"] = []any{map[string]any{
			"type": "Action.OpenUrl", "title": "Create a template", "url": strings.TrimRight(adminURL, "/") + path,
		}}
	}
	return json.Marshal(card)
}

// HintText is HintCard as a line of Markdown, for a transport that has no room
// for a second card.
func HintText(adminURL, path string) string {
	if adminURL == "" {
		return "_" + HintNoTemplate + " Create one in the teamster admin UI._"
	}
	return "_" + HintNoTemplate + "_ [Create a template](" + strings.TrimRight(adminURL, "/") + path + ")"
}
