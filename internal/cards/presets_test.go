package cards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/teamsv2"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func TestPresetsCoverEverySource(t *testing.T) {
	t.Parallel()

	var keys []string
	for _, p := range Presets() {
		keys = append(keys, p.Key)
		if p.LabelKey == "" || p.Name == "" {
			t.Errorf("preset %q has no label key or name", p.Key)
		}
		if err := templates.Validate(p.Template()); err != nil {
			t.Errorf("preset %q is not a valid template: %v", p.Key, err)
		}
		if !slices.Equal(p.Sources, []string{p.Key}) {
			t.Errorf("preset %q handles %v, want only its own source", p.Key, p.Sources)
		}
	}
	if !slices.Equal(keys, models.AllSources()) {
		t.Errorf("presets = %v, want one per source %v", keys, models.AllSources())
	}
	if _, ok := PresetFor("nope"); ok {
		t.Error("PresetFor found a preset for an unknown source")
	}
}

// Template returns a copy, so storing it cannot change the next caller's preset.
func TestPresetTemplateCopiesSources(t *testing.T) {
	t.Parallel()

	p, _ := PresetFor(models.SourceUniversal)
	tpl := p.Template()
	tpl.Sources[0] = "changed"
	if again, _ := PresetFor(models.SourceUniversal); again.Sources[0] != models.SourceUniversal {
		t.Error("changing a preset's template changed the preset")
	}
}

func TestPresetsRender(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 2, 9, 8, 15, 0, 0, time.UTC)
	firing := models.Event{
		Source: models.SourceAlertmanager, State: models.StateOpen,
		Labels: map[string]string{"alertname": "HighCPU", "severity": "critical", models.SourceLabel: "alertmanager"},
		Alertmanager: &models.AlertmanagerEvent{
			Annotations: map[string]string{"summary": "CPU above 90%", "description": "api is spiking", "runbook_url": "https://example.test/rb"},
			StartsAt:    start, GeneratorURL: "http://prometheus.example/rule",
		},
	}
	resolved := firing
	resolved.State = models.StateClosed
	resolvedAM := *firing.Alertmanager
	resolvedAM.EndsAt = start.Add(time.Hour)
	resolvedAM.GeneratorURL = ""
	resolved.Alertmanager = &resolvedAM
	emptyAM := models.Event{Source: models.SourceAlertmanager, Alertmanager: &models.AlertmanagerEvent{}}

	universal := models.Event{
		Source: models.SourceUniversal, State: models.StateOpen, Labels: map[string]string{"alertname": "HighMemory"},
		Universal: &models.UniversalEvent{URL: "custom"},
	}
	direct := models.Event{Source: models.SourceUniversal, Title: "Deploy finished", Text: "worker **v1.4.2**", Universal: &models.UniversalEvent{}}
	emptyUniversal := models.Event{Source: models.SourceUniversal, Universal: &models.UniversalEvent{}}
	withCard := direct
	withCard.Card = json.RawMessage(`{"type":"AdaptiveCard","version":"1.4","body":[]}`)

	cases := []struct {
		name      string
		source    string
		alert     models.Event
		payload   any
		wantTitle string
		wantCard  bool
		wantText  string
		inCard    []string
	}{
		{name: "alertmanager firing", source: models.SourceAlertmanager, alert: firing, wantTitle: "Firing: CPU above 90%", wantCard: true,
			inCard: []string{`"attention"`, `"HighCPU"`, "Show source", "Runbook", "2026-02-09 08:15:00 UTC"}},
		{name: "alertmanager resolved", source: models.SourceAlertmanager, alert: resolved, wantTitle: "Resolved: CPU above 90%", wantCard: true,
			inCard: []string{`"good"`, "Ended", "Runbook"}},
		{name: "alertmanager empty", source: models.SourceAlertmanager, alert: emptyAM, wantTitle: "Firing: Alert", wantCard: true, inCard: []string{"unknown"}},
		{name: "universal alert", source: models.SourceUniversal, alert: universal, wantTitle: "HighMemory", wantCard: true,
			inCard: []string{"Generator", "custom"}},
		{name: "universal direct text", source: models.SourceUniversal, alert: direct, wantTitle: "Deploy finished", wantText: "<strong>v1.4.2</strong>"},
		{name: "universal direct card", source: models.SourceUniversal, alert: withCard, wantTitle: "Deploy finished", wantCard: true},
		{name: "universal empty", source: models.SourceUniversal, alert: emptyUniversal, wantTitle: "Message", wantCard: true},
		{name: "teamsv2 empty", source: models.SourceTeamsV2, wantTitle: "Teams message"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, _ := PresetFor(tc.source)
			msg, err := templates.RenderMessage(p.Template(), templates.RenderData{Event: tc.alert, Now: "now", Payload: tc.payload})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if msg.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", msg.Title, tc.wantTitle)
			}
			if (len(msg.Card) > 0) != tc.wantCard {
				t.Errorf("card = %s, want one: %v", msg.Card, tc.wantCard)
			}
			if !strings.Contains(msg.Text, tc.wantText) {
				t.Errorf("text = %q, want it to contain %q", msg.Text, tc.wantText)
			}
			for _, want := range tc.inCard {
				if !strings.Contains(string(msg.Card), want) {
					t.Errorf("card lacks %q:\n%s", want, msg.Card)
				}
			}
			if strings.Contains(string(msg.Card), models.SourceLabel) {
				t.Error("card shows the source label, which every message from the source carries")
			}
		})
	}
}

// The Teams V2 preset is checked against the payloads a sender actually posts,
// parsed the way the endpoint parses them.
func TestTeamsV2PresetRendersTheSamples(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file     string
		wantCard bool
	}{
		{file: "teamsv2-card.json", wantCard: true},
		{file: "teamsv2-messagecard.json", wantCard: true},
		{file: "teamsv2-text.json"},
	}
	p, _ := PresetFor(models.SourceTeamsV2)
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("..", "..", "samples", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := teamsv2.Parse(body)
			if err != nil {
				t.Fatal(err)
			}
			ev := models.Event{Source: models.SourceTeamsV2, Title: parsed.Title, Text: parsed.Text}
			if len(parsed.Cards) > 0 {
				ev.Card = parsed.Cards[0]
			}
			var payload any
			_ = json.Unmarshal(body, &payload)

			msg, err := templates.RenderMessage(p.Template(), templates.RenderData{Event: ev, Payload: payload})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if (len(msg.Card) > 0) != tc.wantCard {
				t.Errorf("card = %s, want one: %v", msg.Card, tc.wantCard)
			}
			if msg.Title == "" || (msg.Text == "" && len(msg.Card) == 0) {
				t.Errorf("message says nothing: %+v", msg)
			}
		})
	}
}
