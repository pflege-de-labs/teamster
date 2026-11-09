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
	// Sample is the preview sample to show the preset on, when it is not Key.
	Sample string `json:"sample,omitempty"`
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
			Text:     `{{ .Event.Text }}`,
			Body:     universalBody,
			Sources:  []string{models.SourceUniversal},
		},
		{
			Key:      models.SourceTeamsV2,
			LabelKey: "preset.teamsv2",
			Name:     "Teams V2 webhook (default)",
			Title:    `{{ default .Event.Title "Teams message" }}`,
			Text:     `{{ .Event.Text }}`,
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

// Variants are presets the editor offers beside the defaults but a new
// installation is not seeded with: other layouts of a sender's own card for
// when Teams renders it badly (ADR 0099).
func Variants() []Preset {
	return []Preset{
		{
			Key:      "teamsv2-text",
			LabelKey: "preset.teamsv2-text",
			Name:     "Teams V2 webhook message as text",
			Title:    teamsV2PartsTitle,
			Text:     teamsV2TextText,
			Sources:  []string{models.SourceTeamsV2},
			Sample:   "teamsv2-card",
		},
		{
			Key:      "teamsv2-stacked",
			LabelKey: "preset.teamsv2-stacked",
			Name:     "Teams V2 webhook card, facts stacked",
			Title:    teamsV2PartsTitle,
			Text:     `{{ .Event.Text }}`,
			Body:     teamsV2StackedBody,
			Sources:  []string{models.SourceTeamsV2},
			Sample:   "teamsv2-card",
		},
	}
}

// The card's first text is usually its heading, so it becomes the title and
// the text goes on from the second.
const teamsV2PartsTitle = `{{ $p := cardParts .Event.Card }}{{ default (first $p.Texts) (default .Event.Title "Teams message") }}`

// Facts go in a code block, which keeps long values on their own lines; a
// value that contains a fence would end the block early, so it is defused.
const teamsV2TextText = "{{- $p := cardParts .Event.Card }}\n" +
	"{{- with .Event.Text }}{{ . }}\n\n{{ end }}\n" +
	"{{- range rest $p.Texts }}{{ . }}\n\n{{ end }}\n" +
	"{{- with $p.Facts }}```\n" +
	"{{ range . }}{{ trimSuffix \":\" .Title }}: {{ replace \"```\" \"'''\" .Value }}\n{{ end }}" +
	"```\n\n{{ end }}\n" +
	"{{- range $p.Actions }}[{{ .Title }}]({{ .URL }})\n\n{{ end }}"

// Each fact becomes a heading over its value: a FactSet gives the titles a
// column Teams squeezes to a letter a line when the values are long.
const teamsV2StackedBody = `{{ if .Event.Card }}{{ $p := cardParts .Event.Card }}{
  "type": "AdaptiveCard",
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "version": "1.4",
  "body": [
    {{- range $i, $t := $p.Texts }}{{ if $i }},{{ end }}
    { "type": "TextBlock", "wrap": true, "text": {{ toJSON $t }}{{ if not $i }}, "size": "Medium", "weight": "Bolder"{{ end }} }
    {{- end }}
    {{- range $i, $f := $p.Facts }}{{ if or $i $p.Texts }},{{ end }}
    { "type": "TextBlock", "wrap": true, "weight": "Bolder", "spacing": "Medium", "text": {{ toJSON (trimSuffix ":" $f.Title) }} },
    { "type": "TextBlock", "wrap": true, "spacing": "None", "fontType": "Monospace", "text": {{ toJSON $f.Value }} }
    {{- end }}
  ]
  {{- with $p.Actions }},
  "actions": [
    {{- range $i, $a := . }}{{ if $i }},{{ end }}
    { "type": "Action.OpenUrl", "title": {{ toJSON $a.Title }}, "url": {{ toJSON $a.URL }} }
    {{- end }}
  ]
  {{- end }}
}{{ end }}`

const alertmanagerTitle = `{{ if eq .Event.State "closed" }}Resolved{{ else }}Firing{{ end }}: ` +
	`{{ default .Event.Alertmanager.Annotations.summary (default .Event.Alertmanager.CommonAnnotations.summary (default .Event.Labels.alertname "Alert")) }}` +
	`{{ $n := len .Event.Alertmanager.Alerts }}{{ if gt $n 1 }} ({{ $n }} alerts){{ end }}`

// The label loop tracks its first element because JSON wants commas between
// facts and text/template gives a map range no index. A group of several
// alerts lists them instead of one alert's start and end (ADR 0084).
const alertmanagerBody = `{
  "type": "AdaptiveCard",
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "version": "1.4",
  "body": [
    {
      "type": "Container",
      "style": {{ if eq .Event.State "closed" }}"good"{{ else }}"attention"{{ end }},
      "bleed": true,
      "items": [
        {
          "type": "TextBlock",
          "size": "Medium",
          "weight": "Bolder",
          "wrap": true,
          "text": {{ toJSON (default .Event.Alertmanager.Annotations.summary (default .Event.Alertmanager.CommonAnnotations.summary (default .Event.Labels.alertname "Alert"))) }}
        },
        {
          "type": "TextBlock",
          "spacing": "None",
          "isSubtle": true,
          "wrap": true,
          "text": {{ if eq .Event.State "closed" }}"Resolved"{{ else }}{{ toJSON (printf "Firing · %s" (default .Event.Labels.severity "no severity")) }}{{ end }}
        }
      ]
    },
    {
      "type": "TextBlock",
      "wrap": true,
      "text": {{ toJSON (default .Event.Alertmanager.Annotations.description (default .Event.Alertmanager.CommonAnnotations.description "")) }}
    },
    {
      "type": "FactSet",
      "facts": [
        {{- $first := true }}
        {{- range $key, $value := .Event.Labels }}{{ if ne $key "teamster_source" }}
        {{- if not $first }},{{ end }}{{ $first = false }}
        { "title": {{ toJSON $key }}, "value": {{ toJSON $value }} }
        {{- end }}{{ end }}
      ]
    },
    {{- if gt (len .Event.Alertmanager.Alerts) 1 }}
    {
      "type": "FactSet",
      "separator": true,
      "facts": [
        {{- range $i, $alert := .Event.Alertmanager.Alerts }}{{ if $i }},{{ end }}
        { "title": {{ toJSON (default $alert.Labels.alertname "Alert") }}, "value": {{ toJSON (printf "%s · %s" $alert.Status (default $alert.Annotations.summary ($alert.StartsAt.Format "2006-01-02 15:04:05 MST"))) }} }
        {{- end }}
      ]
    }
    {{- else }}
    {
      "type": "FactSet",
      "separator": true,
      "facts": [
        { "title": "Started", "value": {{ if .Event.Alertmanager.StartsAt.IsZero }}"unknown"{{ else }}{{ toJSON (.Event.Alertmanager.StartsAt.Format "2006-01-02 15:04:05 MST") }}{{ end }} }
        {{- if and (eq .Event.State "closed") (not .Event.Alertmanager.EndsAt.IsZero) }},
        { "title": "Ended", "value": {{ toJSON (.Event.Alertmanager.EndsAt.Format "2006-01-02 15:04:05 MST") }} }
        {{- end }}
      ]
    }
    {{- end }}
  ]
  {{- $actions := false }}
  {{- if or .Event.Alertmanager.GeneratorURL .Event.Alertmanager.Annotations.runbook_url }},
  "actions": [
    {{- if .Event.Alertmanager.GeneratorURL }}{{ $actions = true }}
    { "type": "Action.OpenUrl", "title": "Show source", "url": {{ toJSON .Event.Alertmanager.GeneratorURL }} }
    {{- end }}
    {{- if .Event.Alertmanager.Annotations.runbook_url }}{{ if $actions }},{{ end }}
    { "type": "Action.OpenUrl", "title": "Runbook", "url": {{ toJSON .Event.Alertmanager.Annotations.runbook_url }} }
    {{- end }}
  ]
  {{- end }}
}`

const universalTitle = `{{ default .Event.Title (default .Event.Universal.Attributes.summary (default .Event.Labels.alertname "Message")) }}`

// A sender's own card goes out as given; a sender's own text needs no card
// beside it, so the body renders empty; anything else is shown as an alert.
const universalBody = `{{ if .Event.Card }}{{ toJSON .Event.Card }}{{ else if not .Event.Text }}{
  "type": "AdaptiveCard",
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "version": "1.4",
  "body": [
    {
      "type": "Container",
      "style": {{ if eq .Event.State "closed" }}"good"{{ else if eq .Event.State "open" }}"attention"{{ else }}"default"{{ end }},
      "bleed": true,
      "items": [
        {
          "type": "TextBlock",
          "size": "Medium",
          "weight": "Bolder",
          "wrap": true,
          "text": {{ toJSON (default .Event.Universal.Attributes.summary (default .Event.Labels.alertname "Message")) }}
        }
        {{- if .Event.State }},
        {
          "type": "TextBlock",
          "spacing": "None",
          "isSubtle": true,
          "text": {{ toJSON .Event.State }}
        }
        {{- end }}
      ]
    },
    {
      "type": "TextBlock",
      "wrap": true,
      "text": {{ toJSON (default .Event.Universal.Attributes.description "") }}
    },
    {
      "type": "FactSet",
      "facts": [
        {{- $first := true }}
        {{- range $key, $value := .Event.Labels }}{{ if ne $key "teamster_source" }}
        {{- if not $first }},{{ end }}{{ $first = false }}
        { "title": {{ toJSON $key }}, "value": {{ toJSON $value }} }
        {{- end }}{{ end }}
        {{- if .Event.Universal.URL }}{{ if not $first }},{{ end }}
        { "title": "Generator", "value": {{ toJSON .Event.Universal.URL }} }
        {{- end }}
      ]
    }
  ]
}{{ end }}`

// Parse has already turned a MessageCard into an Adaptive Card, so the first
// card covers both card shapes; a text-only payload needs none.
const teamsV2Body = `{{ if .Event.Card }}{{ toJSON .Event.Card }}{{ end }}`
