package cards

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func sampleData() templates.RenderData {
	return templates.RenderData{
		Alert: models.Alert{
			Status:      "firing",
			Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
			Annotations: map[string]string{"summary": "CPU spiking"},
			StartsAt:    time.Date(2026, 2, 9, 9, 0, 0, 0, time.UTC),
		},
		Now: "2026-02-09T10:00:00Z",
	}
}

// A palette that inserts something the renderer rejects is worse than no
// palette, so every snippet is rendered here.
func TestSnippetsRender(t *testing.T) {
	t.Parallel()

	for _, snippet := range Snippets() {
		t.Run(snippet.Name, func(t *testing.T) {
			t.Parallel()

			if snippet.Label == "" || snippet.Help == "" {
				t.Errorf("snippet %q has no label or help", snippet.Name)
			}

			// A snippet is an element, so it is rendered inside a card to be
			// checked the way it will actually be used.
			card := `{"type":"AdaptiveCard","version":"1.4","body":[` + snippet.Body + `]}`
			rendered, err := templates.Render(card, sampleData())
			if err != nil {
				t.Fatalf("snippet %q does not render: %v", snippet.Name, err)
			}

			var decoded map[string]any
			if err := json.Unmarshal(rendered, &decoded); err != nil {
				t.Errorf("snippet %q renders to invalid JSON: %v", snippet.Name, err)
			}
		})
	}
}

// A conditional snippet renders one way or the other, and both have to be
// valid: the resolved branch is the one nobody tries until an alert clears.
func TestConditionalSnippetRendersBothWays(t *testing.T) {
	t.Parallel()

	var conditional Snippet
	for _, snippet := range Snippets() {
		if snippet.Name == "conditional" {
			conditional = snippet
		}
	}
	if conditional.Body == "" {
		t.Fatal("the conditional snippet is gone")
	}

	card := `{"type":"AdaptiveCard","version":"1.4","body":[` + conditional.Body + `]}`
	for _, status := range []string{"firing", "resolved"} {
		data := sampleData()
		alert := data.Alert.(models.Alert)
		alert.Status = status
		data.Alert = alert

		if _, err := templates.Render(card, data); err != nil {
			t.Errorf("the conditional snippet does not render for a %s alert: %v", status, err)
		}
	}
}

// The starter is a whole card, and it is the first thing anyone sees.
func TestStarterRenders(t *testing.T) {
	t.Parallel()

	rendered, err := templates.Render(Starter, sampleData())
	if err != nil {
		t.Fatalf("the starter card does not render: %v", err)
	}

	var card struct {
		Type string           `json:"type"`
		Body []map[string]any `json:"body"`
	}
	if err := json.Unmarshal(rendered, &card); err != nil {
		t.Fatalf("the starter card renders to invalid JSON: %v", err)
	}
	if card.Type != "AdaptiveCard" || len(card.Body) == 0 {
		t.Errorf("card = %+v, want an Adaptive Card with something in it", card)
	}
	if !strings.Contains(string(rendered), "CPU spiking") {
		t.Errorf("card = %s, want the alert's own summary in it", rendered)
	}
}

// An alert with nothing set is the case a template is least likely to be tried
// against and most likely to break on.
func TestStarterSurvivesAnEmptyAlert(t *testing.T) {
	t.Parallel()

	if _, err := templates.Render(Starter, templates.RenderData{Alert: models.Alert{}}); err != nil {
		t.Errorf("the starter card does not render for an alert with no labels: %v", err)
	}
}
