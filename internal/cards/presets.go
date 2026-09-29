package cards

import "github.com/pflege-de-labs/teamster/internal/models"

// A Preset is a whole template written for one webhook's payload: what the
// editor offers to start from, and what a new installation is seeded with as
// that webhook's default (ADR 0055).
type Preset struct {
	Key      string   `json:"key"`
	LabelKey string   `json:"label_key"`
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Text     string   `json:"text"`
	Body     string   `json:"body"`
	Sources  []string `json:"sources"`
}

// Template is the preset as a template to store.
func (p Preset) Template() models.Template {
	return models.Template{Name: p.Name, Title: p.Title, Text: p.Text, Body: p.Body, Sources: append([]string(nil), p.Sources...)}
}

// Presets lists one preset per source, in the order AllSources gives them.
func Presets() []Preset {
	return []Preset{
		{
			Key:      models.SourceAlertmanager,
			LabelKey: "preset.alertmanager",
			Name:     "Alertmanager (default)",
			Title:    alertmanagerTitle,
			Body:     alertmanagerBody,
			Sources:  []string{models.SourceAlertmanager},
		},
		{
			Key:      models.SourceUniversal,
			LabelKey: "preset.universal",
			Name:     "Universal webhook (default)",
			Title:    universalTitle,
			Text:     `{{ .Alert.Text }}`,
			Body:     universalBody,
			Sources:  []string{models.SourceUniversal},
		},
		{
			Key:      models.SourceTeamsV2,
			LabelKey: "preset.teamsv2",
			Name:     "Teams V2 webhook (default)",
			Title:    `{{ default .Alert.Title "Teams message" }}`,
			Text:     `{{ .Alert.Text }}`,
			Body:     teamsV2Body,
			Sources:  []string{models.SourceTeamsV2},
		},
	}
}

// PresetFor returns the preset written for source.
func PresetFor(source string) (Preset, bool) {
	for _, p := range Presets() {
		if p.Key == source {
			return p, true
		}
	}
	return Preset{}, false
}

const alertmanagerTitle = `{{ if eq .Alert.Status "resolved" }}Resolved{{ else }}Firing{{ end }}: ` +
	`{{ default .Alert.Annotations.summary (default .Alert.Labels.alertname "Alert") }}`

// The label loop tracks its first element because JSON wants commas between
// facts and text/template gives a map range no index.
const alertmanagerBody = `{
  "type": "AdaptiveCard",
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "version": "1.4",
  "body": [
    {
      "type": "Container",
      "style": {{ if eq .Alert.Status "resolved" }}"good"{{ else }}"attention"{{ end }},
      "bleed": true,
      "items": [
        {
          "type": "TextBlock",
          "size": "Medium",
          "weight": "Bolder",
          "wrap": true,
          "text": {{ toJSON (default .Alert.Annotations.summary (default .Alert.Labels.alertname "Alert")) }}
        },
        {
          "type": "TextBlock",
          "spacing": "None",
          "isSubtle": true,
          "wrap": true,
          "text": {{ if eq .Alert.Status "resolved" }}"Resolved"{{ else }}{{ toJSON (printf "Firing · %s" (default .Alert.Labels.severity "no severity")) }}{{ end }}
        }
      ]
    },
    {
      "type": "TextBlock",
      "wrap": true,
      "text": {{ toJSON (default .Alert.Annotations.description "") }}
    },
    {
      "type": "FactSet",
      "facts": [
        {{- $first := true }}
        {{- range $key, $value := .Alert.Labels }}{{ if ne $key "teamster_source" }}
        {{- if not $first }},{{ end }}{{ $first = false }}
        { "title": {{ toJSON $key }}, "value": {{ toJSON $value }} }
        {{- end }}{{ end }}
      ]
    },
    {
      "type": "FactSet",
      "separator": true,
      "facts": [
        { "title": "Started", "value": {{ if .Alert.StartsAt.IsZero }}"unknown"{{ else }}{{ toJSON (.Alert.StartsAt.Format "2006-01-02 15:04:05 MST") }}{{ end }} }
        {{- if and (eq .Alert.Status "resolved") (not .Alert.EndsAt.IsZero) }},
        { "title": "Ended", "value": {{ toJSON (.Alert.EndsAt.Format "2006-01-02 15:04:05 MST") }} }
        {{- end }}
      ]
    }
  ]
  {{- $actions := false }}
  {{- if or .Alert.Generator .Alert.Annotations.runbook_url }},
  "actions": [
    {{- if .Alert.Generator }}{{ $actions = true }}
    { "type": "Action.OpenUrl", "title": "Show source", "url": {{ toJSON .Alert.Generator }} }
    {{- end }}
    {{- if .Alert.Annotations.runbook_url }}{{ if $actions }},{{ end }}
    { "type": "Action.OpenUrl", "title": "Runbook", "url": {{ toJSON .Alert.Annotations.runbook_url }} }
    {{- end }}
  ]
  {{- end }}
}`

const universalTitle = `{{ default .Alert.Title (default .Alert.Annotations.summary (default .Alert.Labels.alertname "Message")) }}`

// A sender's own card goes out as given; a sender's own text needs no card
// beside it, so the body renders empty; anything else is shown as an alert.
const universalBody = `{{ if .Alert.Card }}{{ toJSON .Alert.Card }}{{ else if not .Alert.Text }}{
  "type": "AdaptiveCard",
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "version": "1.4",
  "body": [
    {
      "type": "Container",
      "style": {{ if eq .Alert.Status "resolved" }}"good"{{ else if eq .Alert.Status "firing" }}"attention"{{ else }}"default"{{ end }},
      "bleed": true,
      "items": [
        {
          "type": "TextBlock",
          "size": "Medium",
          "weight": "Bolder",
          "wrap": true,
          "text": {{ toJSON (default .Alert.Annotations.summary (default .Alert.Labels.alertname "Message")) }}
        }
        {{- if .Alert.Status }},
        {
          "type": "TextBlock",
          "spacing": "None",
          "isSubtle": true,
          "text": {{ toJSON .Alert.Status }}
        }
        {{- end }}
      ]
    },
    {
      "type": "TextBlock",
      "wrap": true,
      "text": {{ toJSON (default .Alert.Annotations.description "") }}
    },
    {
      "type": "FactSet",
      "facts": [
        {{- $first := true }}
        {{- range $key, $value := .Alert.Labels }}{{ if ne $key "teamster_source" }}
        {{- if not $first }},{{ end }}{{ $first = false }}
        { "title": {{ toJSON $key }}, "value": {{ toJSON $value }} }
        {{- end }}{{ end }}
        {{- if .Alert.Generator }}{{ if not $first }},{{ end }}
        { "title": "Generator", "value": {{ toJSON .Alert.Generator }} }
        {{- end }}
      ]
    }
  ]
}{{ end }}`

// Parse has already turned a MessageCard into an Adaptive Card, so the first
// card covers both card shapes; a text-only payload needs none.
const teamsV2Body = `{{ if .Alert.Card }}{{ toJSON .Alert.Card }}{{ end }}`
