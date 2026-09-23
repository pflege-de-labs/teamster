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
// payload. It is best effort -- a readable title, the status and description,
// and the whole alert as JSON -- so a reader can still act on it.
func Default(alert models.Alert) (Message, error) {
	title := alert.Annotations["summary"]
	if title == "" {
		title = alert.Labels["alertname"]
	}
	if title == "" {
		title = "Alert update"
	}

	var text strings.Builder
	if alert.Status != "" {
		fmt.Fprintf(&text, "**Status:** %s\n\n", alert.Status)
	}
	if description := alert.Annotations["description"]; description != "" {
		text.WriteString(description + "\n\n")
	}
	payload, err := json.MarshalIndent(defaultPayload(alert), "", "  ")
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

// defaultPayload is the alert as a sender would recognise it: the fields it
// set, without the ones only direct content uses, and without zero times.
func defaultPayload(alert models.Alert) map[string]any {
	out := map[string]any{}
	add := func(key string, value any, present bool) {
		if present {
			out[key] = value
		}
	}
	add("source", alert.Source, alert.Source != "")
	add("status", alert.Status, alert.Status != "")
	add("labels", alert.Labels, len(alert.Labels) > 0)
	add("annotations", alert.Annotations, len(alert.Annotations) > 0)
	add("starts_at", alert.StartsAt.Format(time.RFC3339), !alert.StartsAt.IsZero())
	add("ends_at", alert.EndsAt.Format(time.RFC3339), !alert.EndsAt.IsZero())
	add("generator", alert.Generator, alert.Generator != "")
	add("fingerprint", alert.Fingerprint, alert.Fingerprint != "")
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
