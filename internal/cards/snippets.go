// Package cards holds the Adaptive Card fragments the template editor offers.
// They live in Go rather than in the browser so that a test can render every
// one of them: a palette that inserts a card the renderer rejects is worse than
// no palette.
//
// Every value that comes from an event is written as `{{ toJSON … }}` without
// surrounding quotes, rather than as `"{{ … }}"`. toJSON emits the quotes and
// escapes what is inside them, so an event whose title contains a quotation
// mark produces a card instead of broken JSON.
//
// A fragment reads a webhook's extension only under with, because it may be
// dropped into a template for any source (ADR 0056).
package cards

// A Snippet is one thing an operator can drop into a template. Body is a card
// element, not a whole card, except for Starter.
//
// LabelKey and HelpKey are catalog keys rather than text: the palette is part
// of the UI, and the UI is translated. The fragment itself is not — it is JSON
// and Go template syntax, which do not vary by language.
type Snippet struct {
	Name     string `json:"name"`
	LabelKey string `json:"label_key"`
	HelpKey  string `json:"help_key"`
	Body     string `json:"body"`
}

// Starter is what a new template begins as: a whole card that renders against
// any event, showing the shape of the thing rather than an empty box. Every
// field is there to be deleted, which is easier than remembering what is
// available.
const Starter = `{
  "type": "AdaptiveCard",
  "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
  "version": "1.4",
  "body": [
    {
      "type": "TextBlock",
      "size": "Medium",
      "weight": "Bolder",
      "wrap": true,
      "text": {{ toJSON (default .Event.Title (default .Event.Labels.alertname "Event")) }}
    },
    {
      "type": "FactSet",
      "facts": [
        { "title": "Source", "value": {{ toJSON .Event.Source }} },
        { "title": "State", "value": {{ toJSON (default (print .Event.State) "none") }} },
        { "title": "Severity", "value": {{ toJSON (default .Event.Labels.severity "unset") }} }
      ]
    }
    {{- with .Event.Alertmanager }},
    {
      "type": "TextBlock",
      "wrap": true,
      "isSubtle": true,
      "text": {{ toJSON (default .Annotations.description "") }}
    }
    {{- end }}
    {{- with .Event.Universal }},
    {
      "type": "TextBlock",
      "wrap": true,
      "isSubtle": true,
      "text": {{ toJSON (default .Attributes.description "") }}
    }
    {{- end }}
  ]
}`

// Snippets are the elements this service's own templates actually use. The list
// is deliberately short: an editor that offers every element Adaptive Cards has
// is a worse version of the documentation.
func Snippets() []Snippet {
	return []Snippet{
		{
			Name:     "text",
			LabelKey: "palette.text",
			HelpKey:  "palette.text_help",
			Body: `{
  "type": "TextBlock",
  "wrap": true,
  "text": {{ toJSON .Event.Labels.alertname }}
}`,
		},
		{
			Name:     "facts",
			LabelKey: "palette.facts",
			HelpKey:  "palette.facts_help",
			Body: `{
  "type": "FactSet",
  "facts": [
    { "title": "State", "value": {{ toJSON .Event.State }} },
    { "title": "Severity", "value": {{ toJSON (default .Event.Labels.severity "unset") }} }
  ]
}`,
		},
		{
			Name:     "columns",
			LabelKey: "palette.columns",
			HelpKey:  "palette.columns_help",
			Body: `{
  "type": "ColumnSet",
  "columns": [
    {
      "type": "Column",
      "width": "stretch",
      "items": [
        { "type": "TextBlock", "wrap": true, "text": {{ toJSON .Event.Labels.alertname }} }
      ]
    },
    {
      "type": "Column",
      "width": "auto",
      "items": [
        { "type": "TextBlock", "wrap": true, "text": {{ toJSON .Event.State }} }
      ]
    }
  ]
}`,
		},
		{
			Name:     "link",
			LabelKey: "palette.link",
			HelpKey:  "palette.link_help",
			Body: `{
  "type": "ActionSet",
  "actions": [
    {
      "type": "Action.OpenUrl",
      "title": "Runbook",
      "url": {{ with .Event.Alertmanager }}{{ toJSON (default .Annotations.runbook_url "https://example.invalid") }}{{ else }}"https://example.invalid"{{ end }}
    }
  ]
}`,
		},
		{
			Name:     "labels",
			LabelKey: "palette.labels",
			HelpKey:  "palette.labels_help",
			Body: `{
  "type": "TextBlock",
  "wrap": true,
  "fontType": "Monospace",
  "text": {{ toJSON (toJSON .Event.Labels) }}
}`,
		},
		{
			Name:     "conditional",
			LabelKey: "palette.conditional",
			HelpKey:  "palette.conditional_help",
			Body: `{{ if eq .Event.State "closed" }}
{
  "type": "TextBlock",
  "wrap": true,
  "color": "Good",
  "text": {{ toJSON (print "Closed as of " .Now) }}
}
{{ else }}
{
  "type": "TextBlock",
  "wrap": true,
  "color": "Attention",
  "text": {{ toJSON (print "Open as of " .Now) }}
}
{{ end }}`,
		},
	}
}
