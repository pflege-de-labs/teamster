package templates

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestCardParts(t *testing.T) {
	t.Parallel()

	nested := `{"type":"AdaptiveCard","body":[
		{"type":"TextBlock","text":"Exception occurred"},
		{"type":"Container","items":[
			{"type":"ColumnSet","columns":[
				{"type":"Column","items":[{"type":"TextBlock","text":"  in a column  "}]}
			]},
			{"type":"FactSet","facts":[{"title":"Function:","value":"push"},{"title":"User:"}]}
		]},
		{"type":"Table","rows":[{"type":"TableRow","cells":[{"type":"TableCell","items":[{"type":"TextBlock","text":"cell"}]}]}]},
		{"type":"RichTextBlock","inlines":["plain ",{"type":"TextRun","text":"run"}]},
		{"type":"TextBlock","text":"   "},
		{"type":"ActionSet","actions":[{"type":"Action.OpenUrl","title":"Logs","url":"https://logs.test"}]},
		"not an element"
	],"actions":[
		{"type":"Action.OpenUrl","title":"Runbook","url":"https://runbook.test"},
		{"type":"Action.Submit","title":"Ack"},
		{"type":"Action.OpenUrl","title":"No URL"}
	]}`
	want := CardParts{
		Texts:   []string{"Exception occurred", "in a column", "cell", "plain run"},
		Facts:   []Fact{{Title: "Function:", Value: "push"}, {Title: "User:"}},
		Actions: []Action{{Title: "Logs", URL: "https://logs.test"}, {Title: "Runbook", URL: "https://runbook.test"}},
	}
	var asMap map[string]any
	if err := json.Unmarshal([]byte(nested), &asMap); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		card any
		want CardParts
	}{
		{name: "no card", card: nil},
		{name: "empty raw card", card: json.RawMessage(nil)},
		{name: "not JSON", card: []byte("garbage")},
		{name: "not a card", card: 42},
		{name: "raw card", card: json.RawMessage(nested), want: want},
		{name: "bytes", card: []byte(nested), want: want},
		{name: "decoded map", card: asMap, want: want},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := cardParts(tt.card); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cardParts() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A template reaches the parts of the event's card, and one written for a
// card still renders when the message brought none.
func TestCardPartsInATemplate(t *testing.T) {
	t.Parallel()

	const body = `{{ $p := cardParts .Event.Card }}{"title":{{ toJSON (default (first $p.Texts) "none") }},"facts":{{ len $p.Facts }}}`
	tests := []struct {
		name string
		card json.RawMessage
		want string
	}{
		{
			name: "a card",
			card: json.RawMessage(`{"body":[{"type":"TextBlock","text":"Hi"},{"type":"FactSet","facts":[{"title":"a","value":"b"}]}]}`),
			want: `{"title":"Hi","facts":1}`,
		},
		{name: "no card", want: `{"title":"none","facts":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Render(body, RenderData{Event: models.Event{Card: tt.card}})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Render() = %s, want %s", got, tt.want)
			}
		})
	}
}
