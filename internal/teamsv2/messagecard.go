package teamsv2

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The card this service emits. 1.4 is what Teams renders everywhere the old
// connector cards used to appear.
const (
	cardSchema  = "http://adaptivecards.io/schemas/adaptive-card.json"
	cardVersion = "1.4"
)

type adaptiveCard struct {
	Schema  string        `json:"$schema"`
	Type    string        `json:"type"`
	Version string        `json:"version"`
	Body    []cardElement `json:"body,omitempty"`
	Actions []cardOpenURL `json:"actions,omitempty"`
}

// cardElement is every element kind this converter emits, in one struct.
// Adaptive Cards discriminates on "type" and ignores what it does not expect,
// so omitempty is what keeps each element to its own fields.
type cardElement struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	Wrap     bool          `json:"wrap,omitempty"`
	Weight   string        `json:"weight,omitempty"`
	Size     string        `json:"size,omitempty"`
	IsSubtle bool          `json:"isSubtle,omitempty"`
	Spacing  string        `json:"spacing,omitempty"`
	URL      string        `json:"url,omitempty"`
	AltText  string        `json:"altText,omitempty"`
	Style    string        `json:"style,omitempty"`
	Facts    []cardFact    `json:"facts,omitempty"`
	Items    []cardElement `json:"items,omitempty"`
	Columns  []cardColumn  `json:"columns,omitempty"`
}

type cardColumn struct {
	Type  string        `json:"type"`
	Width string        `json:"width,omitempty"`
	Items []cardElement `json:"items,omitempty"`
}

type cardFact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type cardOpenURL struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// fromMessageCard converts a connector card into one Adaptive Card.
//
// The title becomes the message title rather than a block inside the card,
// because that is the line the Teams activity feed previews and because the
// delivery path already renders a title above the card it sends. Repeating it
// inside would show it twice.
func fromMessageCard(mc MessageCard) (Message, error) {
	msg := Message{Title: oneLine(firstNonEmpty(mc.Title, mc.Summary))}

	var body []cardElement
	if text := strings.TrimSpace(mc.Text); text != "" {
		body = append(body, textBlock(text))
	}
	for _, section := range mc.Sections {
		body = append(body, sectionElements(section)...)
	}

	// A card with a colour and nothing else still says something -- the colour
	// is what a reader of a connector card reacts to first -- but a card with
	// neither text nor sections nor actions says nothing at all.
	actions := openURLActions(mc.PotentialAction)
	if len(body) == 0 && len(actions) == 0 {
		if msg.Title == "" {
			return Message{}, ErrEmpty
		}
		return msg, nil
	}

	// themeColor survives as a container style. Adaptive Cards has no arbitrary
	// colour, so the hue is mapped onto the five it does have; the alternative
	// is dropping the one signal that says "this one is bad" at a glance.
	if style := containerStyle(mc.ThemeColor); style != "" {
		body = []cardElement{{Type: "Container", Style: style, Items: body}}
	}

	card, err := json.Marshal(adaptiveCard{
		Schema:  cardSchema,
		Type:    "AdaptiveCard",
		Version: cardVersion,
		Body:    body,
		Actions: actions,
	})
	if err != nil {
		return Message{}, fmt.Errorf("encode card: %w", err)
	}
	msg.Cards = []json.RawMessage{card}
	return msg, nil
}

func sectionElements(s Section) []cardElement {
	var activity []cardElement
	if s.ActivityTitle != "" {
		activity = append(activity, cardElement{Type: "TextBlock", Text: s.ActivityTitle, Wrap: true, Weight: "bolder"})
	}
	if s.ActivitySubtitle != "" {
		activity = append(activity, cardElement{Type: "TextBlock", Text: s.ActivitySubtitle, Wrap: true, IsSubtle: true, Spacing: "none"})
	}
	if s.ActivityText != "" {
		activity = append(activity, textBlock(s.ActivityText))
	}

	var out []cardElement
	if s.Title != "" {
		out = append(out, cardElement{Type: "TextBlock", Text: s.Title, Wrap: true, Weight: "bolder", Size: "medium"})
	}

	switch {
	// An activity image is a person beside their activity, which is a
	// ColumnSet. Without one the same blocks stack.
	case s.ActivityImage != "" && len(activity) > 0:
		out = append(out, cardElement{Type: "ColumnSet", Columns: []cardColumn{
			{Type: "Column", Width: "auto", Items: []cardElement{{
				Type: "Image", URL: s.ActivityImage, Style: "person", Size: "small", AltText: s.ActivityTitle,
			}}},
			{Type: "Column", Width: "stretch", Items: activity},
		}})
	case s.ActivityImage != "":
		out = append(out, cardElement{Type: "Image", URL: s.ActivityImage, Style: "person", Size: "small"})
	default:
		out = append(out, activity...)
	}

	if text := strings.TrimSpace(s.Text); text != "" {
		out = append(out, textBlock(text))
	}
	if len(s.Facts) > 0 {
		facts := make([]cardFact, 0, len(s.Facts))
		for _, f := range s.Facts {
			facts = append(facts, cardFact{Title: f.Name, Value: f.Value})
		}
		out = append(out, cardElement{Type: "FactSet", Facts: facts})
	}
	for _, img := range s.Images {
		if img.Image == "" {
			continue
		}
		out = append(out, cardElement{Type: "Image", URL: img.Image, AltText: img.Title})
	}
	return out
}

// openURLActions keeps the one action kind that still works. HttpPOST,
// ActionCard and InvokeAddInCommand all need the connector to call the sender
// back, and nothing here can: they are dropped rather than rendered as buttons
// that do nothing.
func openURLActions(actions []CardAction) []cardOpenURL {
	var out []cardOpenURL
	for _, a := range actions {
		if !strings.EqualFold(a.Type, "OpenUri") {
			continue
		}
		for _, t := range a.Targets {
			if t.URI == "" {
				continue
			}
			out = append(out, cardOpenURL{Type: "Action.OpenUrl", Title: firstNonEmpty(a.Name, t.URI), URL: t.URI})
			break
		}
	}
	return out
}

// textBlock carries the MessageCard's markdown through unchanged: a TextBlock
// renders the same subset, so the text arrives looking as it did. A section
// asking for "markdown": false cannot be honoured, because a TextBlock has no
// way to be told not to.
func textBlock(text string) cardElement {
	return cardElement{Type: "TextBlock", Text: text, Wrap: true}
}

// containerStyle maps a connector card's colour onto the five Adaptive Cards
// has. An unparseable or greyish colour gets none, which reads as the plain
// card it would have been anyway.
func containerStyle(themeColor string) string {
	r, g, b, ok := parseHexColor(themeColor)
	if !ok {
		return ""
	}

	hue, saturation := hueSaturation(r, g, b)
	if saturation < 0.15 {
		return ""
	}
	switch {
	case hue < 20 || hue >= 330:
		return "attention"
	case hue < 70:
		return "warning"
	case hue < 170:
		return "good"
	case hue < 290:
		return "accent"
	default:
		return "attention"
	}
}

func parseHexColor(s string) (r, g, b float64, ok bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	value, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64((value >> 16) & 0xff), float64((value >> 8) & 0xff), float64(value & 0xff), true
}

func hueSaturation(r, g, b float64) (hue, saturation float64) {
	maxC := math.Max(r, math.Max(g, b))
	minC := math.Min(r, math.Min(g, b))
	span := maxC - minC
	if maxC == 0 || span == 0 {
		return 0, 0
	}

	switch maxC {
	case r:
		hue = math.Mod((g-b)/span, 6)
	case g:
		hue = (b-r)/span + 2
	default:
		hue = (r-g)/span + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}
	return hue, span / maxC
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
