// Package teamsv2 reads the request bodies a Microsoft Teams webhook accepts
// and turns them into the three parts this service sends: a title, some text
// and Adaptive Cards.
//
// It exists so that a sender already posting to a Teams "Workflows" (Power
// Automate) webhook can be re-pointed here without changing what it sends.
// Three shapes are in the wild and all three are accepted: the V2 envelope, a
// bare text body, and the MessageCard an O365 connector took before connectors
// were retired.
package teamsv2

import "encoding/json"

// AdaptiveCardContentType is the attachment content type Teams uses for an
// Adaptive Card, in the envelope and on the way out to Graph alike.
const AdaptiveCardContentType = "application/vnd.microsoft.card.adaptive"

// Envelope is the V2 payload: a message with attached cards.
type Envelope struct {
	Type        string       `json:"type"`
	Summary     string       `json:"summary"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

// Attachment keeps its content raw. A card is forwarded to Graph exactly as it
// arrived -- passing it through untouched is the whole point of accepting this
// shape, and re-encoding it would drop whatever this service does not model.
type Attachment struct {
	ContentType string          `json:"contentType"`
	Content     json.RawMessage `json:"content"`
}

// MessageCard is the legacy O365 connector card.
type MessageCard struct {
	Type            string       `json:"@type"`
	ThemeColor      string       `json:"themeColor"`
	Summary         string       `json:"summary"`
	Title           string       `json:"title"`
	Text            string       `json:"text"`
	Sections        []Section    `json:"sections"`
	PotentialAction []CardAction `json:"potentialAction"`
}

type Section struct {
	Title            string  `json:"title"`
	Text             string  `json:"text"`
	ActivityTitle    string  `json:"activityTitle"`
	ActivitySubtitle string  `json:"activitySubtitle"`
	ActivityImage    string  `json:"activityImage"`
	ActivityText     string  `json:"activityText"`
	Facts            []Fact  `json:"facts"`
	Images           []Image `json:"images"`
}

type Fact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Image struct {
	Image string `json:"image"`
	Title string `json:"title"`
}

type CardAction struct {
	Type    string   `json:"@type"`
	Name    string   `json:"name"`
	Targets []Target `json:"targets"`
}

type Target struct {
	OS  string `json:"os"`
	URI string `json:"uri"`
}
