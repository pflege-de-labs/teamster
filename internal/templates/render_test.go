package templates

import (
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestRenderValidJSON(t *testing.T) {
	body := `{"type":"AdaptiveCard","version":"1.4","body":[{"type":"TextBlock","text":"{{ default (index .Alert "alertname") "unknown" }}"}]}`

	payload, err := Render(body, RenderData{Alert: map[string]string{"alertname": "HighCPU"}})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(payload) == 0 {
		t.Fatalf("expected payload bytes")
	}
}

func TestRenderInvalidJSON(t *testing.T) {
	body := `{"type":"AdaptiveCard","body":[` // invalid JSON after template render

	_, err := Render(body, RenderData{Alert: map[string]string{"alertname": "HighCPU"}})
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestRenderErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		data    RenderData
		wantErr string
	}{
		{
			name:    "template does not parse",
			body:    `{"text":"{{ .Alert"}`,
			wantErr: "parse template",
		},
		{
			name:    "template execution fails",
			body:    `{"text":"{{ toJSON .Alert }}"}`,
			data:    RenderData{Alert: make(chan int)},
			wantErr: "execute template",
		},
		{
			name:    "output is not JSON",
			body:    `not json`,
			wantErr: "template output is not valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Render(tt.body, tt.data)
			if err == nil {
				t.Fatalf("Render() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Render() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRenderHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		data RenderData
		want string
	}{
		{
			name: "toJSON encodes the alert",
			body: `{"alert":{{ toJSON .Alert }}}`,
			data: RenderData{Alert: map[string]string{"severity": "critical"}},
			want: `{"alert":{"severity":"critical"}}`,
		},
		{
			name: "default fills in an empty value",
			body: `{"text":"{{ default "" "fallback" }}"}`,
			want: `{"text":"fallback"}`,
		},
		{
			name: "default keeps a set value",
			body: `{"text":"{{ default "set" "fallback" }}"}`,
			want: `{"text":"set"}`,
		},
		{
			name: "Now is exposed to the template",
			body: `{"time":"{{ .Now }}"}`,
			data: RenderData{Now: "2026-09-08T10:00:00Z"},
			want: `{"time":"2026-09-08T10:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Render(tt.body, tt.data)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Render() = %s, want %s", got, tt.want)
			}
		})
	}
}

// RenderText is renderMarkdown+Sanitize, split out of RenderMessage's own
// text step so a sender's own text -- never authored as Go template source --
// still goes through the exact same trust boundary a stored template's text
// does.
func TestRenderText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain text", in: "deployment finished", want: "<p>deployment finished</p>"},
		{name: "markdown", in: "**bold**", want: "<p><strong>bold</strong></p>"},
		{
			name: "a script tag does not survive, same trust boundary as a template's text",
			in:   "before<script>alert(1)</script>after",
			want: "<p>beforeafter</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RenderText(tt.in)
			if err != nil {
				t.Fatalf("RenderText: %v", err)
			}
			if got != tt.want {
				t.Errorf("RenderText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A body that renders to nothing sends no card, so one template can carry a
// card for some payloads and only text for others (ADR 0055).
func TestRenderMessageSkipsAnEmptyCard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		alert    map[string]string
		wantCard bool
	}{
		{name: "card", alert: map[string]string{"card": "yes"}, wantCard: true},
		{name: "no card", alert: map[string]string{}},
	}
	tmpl := models.Template{Title: "t", Body: `{{ if .Alert.card }}{"type":"AdaptiveCard"}{{ end }}` + "\n  "}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := RenderMessage(tmpl, RenderData{Alert: tt.alert})
			if err != nil {
				t.Fatalf("RenderMessage: %v", err)
			}
			if (len(msg.Card) > 0) != tt.wantCard {
				t.Errorf("card = %s, want one: %v", msg.Card, tt.wantCard)
			}
		})
	}
	if _, err := Render(tmpl.Body, RenderData{Alert: map[string]string{}}); err == nil {
		t.Error("Render accepted an empty card; only RenderMessage may skip one")
	}
}
