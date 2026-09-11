// Package cards holds the Adaptive Card fragments the template editor offers.
// They live in Go rather than in the browser so that a test can render every
// one of them: a palette that inserts a card the renderer rejects is worse than
// no palette.
//
// Every value that comes from an alert is written as `{{ toJSON … }}` without
// surrounding quotes, rather than as `"{{ … }}"`. toJSON emits the quotes and
// escapes what is inside them, so an alert whose summary contains a quotation
// mark produces a card instead of broken JSON.
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
// any alert, showing the shape of the thing rather than an empty box. Every
// field an alert carries is there to be deleted, which is easier than
// remembering what is available.
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
      "text": {{ toJSON (default .Alert.Annotations.summary .Alert.Labels.alertname) }}
    },
    {
      "type": "FactSet",
      "facts": [
        { "title": "Status", "value": {{ toJSON .Alert.Status }} },
        { "title": "Severity", "value": {{ toJSON (default .Alert.Labels.severity "unset") }} },
        { "title": "Started", "value": {{ toJSON .Alert.StartsAt }} }
      ]
    },
    {
      "type": "TextBlock",
      "wrap": true,
      "isSubtle": true,
      "text": {{ toJSON (default .Alert.Annotations.description "") }}
    }
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
  "text": {{ toJSON .Alert.Labels.alertname }}
}`,
		},
		{
			Name:     "facts",
			LabelKey: "palette.facts",
			HelpKey:  "palette.facts_help",
			Body: `{
  "type": "FactSet",
  "facts": [
    { "title": "Status", "value": {{ toJSON .Alert.Status }} },
    { "title": "Severity", "value": {{ toJSON (default .Alert.Labels.severity "unset") }} }
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
        { "type": "TextBlock", "wrap": true, "text": {{ toJSON .Alert.Labels.alertname }} }
      ]
    },
    {
      "type": "Column",
      "width": "auto",
      "items": [
        { "type": "TextBlock", "wrap": true, "text": {{ toJSON .Alert.Status }} }
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
      "url": {{ toJSON (default .Alert.Annotations.runbook_url "https://example.invalid") }}
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
  "text": {{ toJSON (toJSON .Alert.Labels) }}
}`,
		},
		{
			Name:     "conditional",
			LabelKey: "palette.conditional",
			HelpKey:  "palette.conditional_help",
			Body: `{{ if eq .Alert.Status "firing" }}
{
  "type": "TextBlock",
  "wrap": true,
  "color": "Attention",
  "text": {{ toJSON (print "Firing since " .Alert.StartsAt) }}
}
{{ else }}
{
  "type": "TextBlock",
  "wrap": true,
  "color": "Good",
  "text": {{ toJSON (print "Resolved at " .Alert.EndsAt) }}
}
{{ end }}`,
		},
	}
}
