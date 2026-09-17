package teamsv2

import (
	"testing"
)

// fullCard is the shape a connector card actually arrives in: a theme colour, a
// section with an activity and facts, and an action.
const fullCard = `{
	"@type": "MessageCard",
	"@context": "http://schema.org/extensions",
	"themeColor": "D70000",
	"summary": "Disk almost full",
	"title": "Disk almost full",
	"text": "node-3 is at 94%",
	"sections": [{
		"activityTitle": "prometheus",
		"activitySubtitle": "2 minutes ago",
		"activityImage": "https://example.test/avatar.png",
		"activityText": "threshold crossed",
		"title": "Details",
		"text": "the volume backing /var",
		"facts": [
			{"name": "severity", "value": "critical"},
			{"name": "cluster", "value": "eu-1"}
		],
		"images": [{"image": "https://example.test/graph.png", "title": "last hour"}]
	}],
	"potentialAction": [
		{"@type": "OpenUri", "name": "Open runbook", "targets": [{"os": "default", "uri": "https://example.test/runbook"}]},
		{"@type": "HttpPOST", "name": "Acknowledge", "target": "https://example.test/ack"}
	]
}`

func TestMessageCardConversion(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(fullCard))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if msg.Title != "Disk almost full" {
		t.Errorf("Title = %q, want the card's title", msg.Title)
	}
	// The title is the line above the card, not a block inside it: the delivery
	// path already renders it, and repeating it would show it twice.
	if msg.Text != "" {
		t.Errorf("Text = %q, want the card to carry the body", msg.Text)
	}
	if len(msg.Cards) != 1 {
		t.Fatalf("len(Cards) = %d, want 1", len(msg.Cards))
	}

	card := decodeCard(t, msg.Cards[0])
	if card["type"] != "AdaptiveCard" {
		t.Errorf("type = %v, want AdaptiveCard", card["type"])
	}
	if card["version"] != cardVersion {
		t.Errorf("version = %v, want %s", card["version"], cardVersion)
	}

	body, ok := card["body"].([]any)
	if !ok || len(body) != 1 {
		t.Fatalf("body = %v, want one container", card["body"])
	}
	container, _ := body[0].(map[string]any)
	if container["type"] != "Container" || container["style"] != "attention" {
		t.Errorf("container = %v, want a Container styled attention", container)
	}

	kinds := elementKinds(container["items"])
	want := []string{"TextBlock", "TextBlock", "ColumnSet", "TextBlock", "FactSet", "Image"}
	if len(kinds) != len(want) {
		t.Fatalf("element kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("element %d = %s, want %s", i, kinds[i], want[i])
		}
	}

	actions, ok := card["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("actions = %v, want only the OpenUri one", card["actions"])
	}
	action, _ := actions[0].(map[string]any)
	if action["type"] != "Action.OpenUrl" || action["url"] != "https://example.test/runbook" {
		t.Errorf("action = %v, want the runbook link", action)
	}
	if action["title"] != "Open runbook" {
		t.Errorf("action title = %v, want the action's name", action["title"])
	}
}

func TestMessageCardFacts(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(`{"@type":"MessageCard","title":"x","sections":[{"facts":[{"name":"a","value":"1"}]}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	card := decodeCard(t, msg.Cards[0])
	body, _ := card["body"].([]any)
	factSet, _ := body[0].(map[string]any)
	facts, ok := factSet["facts"].([]any)
	if !ok || len(facts) != 1 {
		t.Fatalf("facts = %v, want one", factSet["facts"])
	}
	fact, _ := facts[0].(map[string]any)
	// A MessageCard fact is name/value; an Adaptive Card fact is title/value.
	if fact["title"] != "a" || fact["value"] != "1" {
		t.Errorf("fact = %v, want title a and value 1", fact)
	}
}

func TestMessageCardFallsBackToSummaryForTheTitle(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(`{"@type":"MessageCard","summary":"only a summary","text":"body"}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if msg.Title != "only a summary" {
		t.Errorf("Title = %q, want the summary", msg.Title)
	}
}

func TestMessageCardWithoutAnActivityImageStacksTheActivity(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(`{"@type":"MessageCard","sections":[{"activityTitle":"who","activityText":"what"}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	card := decodeCard(t, msg.Cards[0])
	kinds := elementKinds(card["body"])
	if len(kinds) != 2 || kinds[0] != "TextBlock" || kinds[1] != "TextBlock" {
		t.Errorf("element kinds = %v, want two stacked TextBlocks", kinds)
	}
}

func TestContainerStyleFromThemeColour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		color string
		want  string
	}{
		{name: "red", color: "FF0000", want: "attention"},
		{name: "red with a hash", color: "#d70000", want: "attention"},
		{name: "three digits", color: "#f00", want: "attention"},
		{name: "orange", color: "FFA500", want: "warning"},
		{name: "yellow", color: "FFFF00", want: "warning"},
		{name: "green", color: "00FF00", want: "good"},
		{name: "the green a connector card usually used", color: "2DC72D", want: "good"},
		{name: "cyan sits nearer blue than green", color: "008080", want: "accent"},
		{name: "blue", color: "0078D4", want: "accent"},
		{name: "purple", color: "800080", want: "attention"},
		{name: "grey has no hue to map", color: "808080", want: ""},
		{name: "white", color: "FFFFFF", want: ""},
		{name: "black", color: "000000", want: ""},
		{name: "not a colour", color: "cornflower", want: ""},
		{name: "absent", color: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := containerStyle(tt.color); got != tt.want {
				t.Errorf("containerStyle(%q) = %q, want %q", tt.color, got, tt.want)
			}
		})
	}
}

func TestMessageCardWithoutAThemeColourIsNotWrapped(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(`{"@type":"MessageCard","text":"plain"}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	card := decodeCard(t, msg.Cards[0])
	kinds := elementKinds(card["body"])
	if len(kinds) != 1 || kinds[0] != "TextBlock" {
		t.Errorf("element kinds = %v, want the text alone", kinds)
	}
}

func TestMessageCardWithOnlyAnActionStillSends(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(`{"@type":"MessageCard","title":"t","potentialAction":[{"@type":"OpenUri","targets":[{"uri":"https://example.test"}]}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	card := decodeCard(t, msg.Cards[0])
	actions, _ := card["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %v, want one", card["actions"])
	}
	action, _ := actions[0].(map[string]any)
	// With no name to show, the URL is the only label there is.
	if action["title"] != "https://example.test" {
		t.Errorf("action title = %v, want the URL", action["title"])
	}
}

func TestMessageCardWithOnlyAThemeColourAndATitleSendsNoCard(t *testing.T) {
	t.Parallel()

	// Nothing to draw, but a title the activity feed can still show.
	msg, err := Parse([]byte(`{"@type":"MessageCard","themeColor":"FF0000","title":"just a title"}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if msg.Title != "just a title" {
		t.Errorf("Title = %q, want the title", msg.Title)
	}
	if len(msg.Cards) != 0 {
		t.Errorf("Cards = %s, want none", msg.Cards)
	}
}

// elementKinds is the "type" of each element in a card body, which is what the
// conversion tests actually care about.
func elementKinds(body any) []string {
	items, ok := body.([]any)
	if !ok {
		return nil
	}
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		element, _ := item.(map[string]any)
		kind, _ := element["type"].(string)
		kinds = append(kinds, kind)
	}
	return kinds
}
