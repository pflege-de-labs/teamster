package templates

import (
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func sampleAlert() models.Alert {
	return models.Alert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
		Annotations: map[string]string{"summary": "CPU spiking"},
	}
}

func TestRenderMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		template  models.Template
		alert     models.Alert
		wantTitle string
		wantText  string
		wantCard  string
	}{
		{
			name:      "the title is rendered against the alert",
			template:  models.Template{Title: "{{ .Alert.Labels.alertname }} is {{ .Alert.Status }}"},
			alert:     sampleAlert(),
			wantTitle: "HighCPU is firing",
		},
		{
			// The feed shows one line, so a template spanning several must not
			// arrive with newlines in it.
			name:      "a multi-line title collapses",
			template:  models.Template{Title: "{{ .Alert.Labels.alertname }}\n  is  \n{{ .Alert.Status }}"},
			alert:     sampleAlert(),
			wantTitle: "HighCPU is firing",
		},
		{
			name:      "a card without a title gets the one this service always sent",
			template:  models.Template{Body: `{"type":"AdaptiveCard"}`},
			alert:     sampleAlert(),
			wantTitle: "CPU spiking",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			name:      "that fallback walks to the alertname",
			template:  models.Template{Body: `{"type":"AdaptiveCard"}`},
			alert:     models.Alert{Labels: map[string]string{"alertname": "HighCPU"}},
			wantTitle: "HighCPU",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			name:      "and then to a generic line",
			template:  models.Template{Body: `{"type":"AdaptiveCard"}`},
			alert:     models.Alert{},
			wantTitle: "Alert update",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			// Text previews itself in the feed, so inventing a title would only
			// repeat what the reader is about to see.
			name:     "text without a title is left alone",
			template: models.Template{Text: "<p>{{ .Alert.Labels.alertname }}</p>"},
			alert:    sampleAlert(),
			wantText: "<p>HighCPU</p>",
		},
		{
			// The one place ADR 0029 is visible to an existing template: a
			// fragment that is only inline markup is not a CommonMark HTML
			// block, so it becomes the paragraph it always implied. Block-level
			// HTML -- which is what a template that formats anything is made of
			// -- passes through byte for byte, as the case above shows.
			name:      "all three parts render together",
			template:  models.Template{Title: "T", Text: "<b>bold</b>", Body: `{"type":"AdaptiveCard"}`},
			alert:     sampleAlert(),
			wantTitle: "T",
			wantText:  "<p><b>bold</b></p>",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			// Authored as Markdown rather than HTML, which is what ADR 0029
			// makes the documented format.
			name:     "a markdown template renders to html",
			template: models.Template{Text: "**{{ .Alert.Labels.alertname }}** is _{{ .Alert.Status }}_"},
			alert:    sampleAlert(),
			wantText: "<p><strong>HighCPU</strong> is <em>firing</em></p>",
		},
		{
			// The sanitizer still runs after the Markdown parser, so raw HTML
			// passing through is not raw HTML arriving.
			name:     "markdown does not get an annotation around the sanitizer",
			template: models.Template{Text: "{{ .Alert.Annotations.summary }}"},
			alert: models.Alert{Annotations: map[string]string{
				"summary": "<script>alert(1)</script>ok",
			}},
			// No paragraph wrapper: "<script" opens a CommonMark raw-HTML
			// block, so the line reaches Sanitize verbatim -- and Sanitize
			// drops the script with its contents, which is the assertion.
			wantText: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := RenderMessage(tt.template, RenderData{Alert: tt.alert, Now: "2026-02-09T09:00:00Z"})
			if err != nil {
				t.Fatalf("RenderMessage: %v", err)
			}
			if msg.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", msg.Title, tt.wantTitle)
			}
			if msg.Text != tt.wantText {
				t.Errorf("text = %q, want %q", msg.Text, tt.wantText)
			}
			if string(msg.Card) != tt.wantCard {
				t.Errorf("card = %s, want %s", msg.Card, tt.wantCard)
			}
		})
	}
}

func TestRenderMessageReportsWhichPartBroke(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template models.Template
		wantErr  string
	}{
		{name: "title", template: models.Template{Title: "{{"}, wantErr: "title: parse template"},
		{name: "text", template: models.Template{Text: "{{ .Missing.Field }}"}, wantErr: "text: execute template"},
		{name: "card", template: models.Template{Body: "not json"}, wantErr: "not valid JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := RenderMessage(tt.template, RenderData{Alert: sampleAlert()})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to name the %s", err, tt.name)
			}
		})
	}
}

// The fallback is as likely to come from the alert as the value is, and an
// alert that carries neither must still render.
func TestDefaultAcceptsAMissingFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		alert models.Alert
		want  string
	}{
		{
			name:  "the value wins when it is there",
			body:  `{"text":"{{ default .Alert.Annotations.summary .Alert.Labels.alertname }}"}`,
			alert: sampleAlert(),
			want:  `{"text":"CPU spiking"}`,
		},
		{
			name:  "the fallback comes from the alert too",
			body:  `{"text":"{{ default .Alert.Annotations.missing .Alert.Labels.alertname }}"}`,
			alert: sampleAlert(),
			want:  `{"text":"HighCPU"}`,
		},
		{
			name: "neither is there",
			body: `{"text":"{{ default .Alert.Annotations.summary .Alert.Labels.alertname }}"}`,
			want: `{"text":""}`,
		},
		{
			name: "chained to a literal",
			body: `{"text":"{{ default .Alert.Annotations.summary (default .Alert.Labels.alertname "nothing") }}"}`,
			want: `{"text":"nothing"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			card, err := Render(tt.body, RenderData{Alert: tt.alert})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if string(card) != tt.want {
				t.Errorf("card = %s, want %s", card, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template models.Template
		wantErr  bool
	}{
		{name: "a card alone", template: models.Template{Body: "{}"}},
		{name: "a title alone", template: models.Template{Title: "hello"}},
		{name: "text alone", template: models.Template{Text: "hello"}},
		{name: "nothing to send", template: models.Template{Name: "named but empty"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, want error: %v", err, tt.wantErr)
			}
		})
	}
}

// A nil map is what an alert without annotations arrives as, and a fallback
// that cannot survive one is no fallback at all.
func TestDefaultHelperSurvivesMissingKeys(t *testing.T) {
	t.Parallel()

	card, err := Render(`{"text":"{{ default .Alert.Annotations.summary "none" }}"}`, RenderData{Alert: models.Alert{}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := `{"text":"none"}`; string(card) != want {
		t.Errorf("card = %s, want %s", card, want)
	}
}
