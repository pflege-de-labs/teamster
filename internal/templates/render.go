package templates

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// DefaultTitle is the summary line for a card-only template: the extension's
// summary when there is one -- for a group, its common one -- else the
// alertname label. Each extension is read
// under with, because a template for any source meets nil ones.
const DefaultTitle = `{{ $summary := "" }}` +
	`{{ with .Event.Alertmanager }}{{ $summary = default (index .Annotations "summary") (index .CommonAnnotations "summary") }}{{ end }}` +
	`{{ with .Event.Universal }}{{ $summary = index .Attributes "summary" }}{{ end }}` +
	`{{ default $summary (default .Event.Labels.alertname "Update") }}`

type RenderData struct {
	Event any
	Now   string
	// Payload is the request body as decoded JSON, for a sender whose shape
	// Event does not capture -- a Teams V2 MessageCard, say. Nil elsewhere.
	Payload any
	// Recipient is the person a chat message is rendered for, zero for a
	// channel (ADR 0063). A value, not a pointer, so a template reads its
	// fields without a nil check.
	Recipient Person
}

// Person is who a message is rendered for. The fields after Mail are their
// Entra profile, empty for a person the directory does not know (ADR 0086).
type Person struct {
	ID                string
	DisplayName       string
	GivenName         string
	Surname           string
	UPN               string
	Mail              string
	JobTitle          string
	Department        string
	CompanyName       string
	OfficeLocation    string
	EmployeeID        string
	Address           models.Address
	BusinessPhones    []string
	MobilePhone       string
	PreferredLanguage string
	UsageLocation     string
}

// PersonOf is the person a directory user is rendered as.
func PersonOf(u models.DirectoryUser) Person {
	p := u.Profile
	return Person{
		ID: u.AADObjectID, DisplayName: u.DisplayName, GivenName: u.GivenName, Surname: u.Surname,
		UPN: u.UserPrincipalName, Mail: u.Mail,
		JobTitle: p.JobTitle, Department: p.Department, CompanyName: p.CompanyName,
		OfficeLocation: p.OfficeLocation, EmployeeID: p.EmployeeID, Address: p.Address,
		BusinessPhones: p.BusinessPhones, MobilePhone: p.MobilePhone,
		PreferredLanguage: p.PreferredLanguage, UsageLocation: p.UsageLocation,
	}
}

// Message is a template rendered against an event. Title is the line the Teams
// activity feed previews — a message that is only a card previews as "Card" —
// and Card is nil for a template that sends text alone.
//
// Text is sanitized HTML, whatever the template was authored in. A chat
// delivery converts it with ToMarkdown rather than carrying a second field:
// one field means one sanitizer, and the chat path cannot then be given
// something the channel path never checked.
type Message struct {
	Title string          `json:"title,omitempty"`
	Text  string          `json:"text,omitempty"`
	Card  json.RawMessage `json:"card,omitempty"`
	// Notice is a card sent after the message itself: the hint that no
	// template rendered it (see HintCard).
	Notice json.RawMessage `json:"notice,omitempty"`
}

// RenderMessage renders the three parts of a template. The text is rendered
// from Markdown and sanitized here rather than at the Graph client, so every
// caller — channel delivery, chat delivery, preview and any future one — gets
// the same guarantee.
func RenderMessage(t models.Template, data RenderData) (Message, error) {
	var msg Message

	// A card with no title in front of it is what previews as "Card", so one is
	// supplied. Text needs no such help: the feed previews the text itself.
	title := t.Title
	if title == "" && t.Body != "" {
		title = DefaultTitle
	}
	if title != "" {
		rendered, err := renderText(title, data)
		if err != nil {
			return Message{}, fmt.Errorf("title: %w", err)
		}
		// The feed shows one line, so a template that spans several becomes one.
		msg.Title = strings.Join(strings.Fields(rendered), " ")
	}

	if t.Text != "" {
		text, err := renderText(t.Text, data)
		if err != nil {
			return Message{}, fmt.Errorf("text: %w", err)
		}
		safe, err := RenderText(text)
		if err != nil {
			return Message{}, fmt.Errorf("text: %w", err)
		}
		msg.Text = safe
	}

	if t.Body != "" {
		card, err := Render(t.Body, data)
		if err != nil && !errors.Is(err, errEmptyCard) {
			return Message{}, err
		}
		msg.Card = card
	}

	return msg, nil
}

// RenderText turns Markdown into the sanitized HTML every transport requires,
// without going through a Go template first. It is renderMarkdown+Sanitize,
// split out of RenderMessage's own text step and exported so a caller with
// text that was never template source -- a webhook payload's own Text field,
// not something authored in the admin UI -- gets exactly the same trust
// boundary a stored template's Text goes through. Authored as Markdown,
// carried on as HTML: raw HTML passes the parser through untouched, so text
// written before ADR 0029 renders as it always did, and Sanitize is still the
// only trust boundary.
func RenderText(markdown string) (string, error) {
	markup, err := renderMarkdown(markdown)
	if err != nil {
		return "", err
	}
	return Sanitize(markup)
}

// Validate rejects a template that would render to nothing a reader can see.
func Validate(t models.Template) error {
	if t.Title == "" && t.Text == "" && t.Body == "" {
		return fmt.Errorf("a template needs a title, text or a card")
	}
	if _, err := models.NormalizeSources(t.Sources); err != nil {
		return err
	}
	return nil
}

func renderText(body string, data RenderData) (string, error) {
	tmpl, err := template.New("text").Funcs(funcs()).Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// errEmptyCard is a body that rendered to nothing. RenderMessage sends no card
// for it, so one template can carry a card for some payloads and not others.
var errEmptyCard = errors.New("template output is empty, not a card")

func Render(body string, data RenderData) (json.RawMessage, error) {
	tmpl, err := template.New("card").Funcs(funcs()).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	output := buf.Bytes()
	if len(bytes.TrimSpace(output)) == 0 {
		return nil, errEmptyCard
	}
	var tmp any
	if err := json.Unmarshal(output, &tmp); err != nil {
		return nil, fmt.Errorf("template output is not valid JSON: %w", err)
	}

	return json.RawMessage(output), nil
}

// leftOut are in sprig's hermetic map anyway: ago and randInt are not
// repeatable, the crypto functions burn CPU on every preview (ADR 0091).
var leftOut = []string{
	"ago", "randInt",
	"bcrypt", "htpasswd", "genPrivateKey", "derivePassword", "buildCustomCert",
	"genCA", "genCAWithKey", "genSelfSignedCert", "genSelfSignedCertWithKey",
	"genSignedCert", "genSignedCertWithKey", "encryptAES", "decryptAES",
}

// funcs is sprig's hermetic set, which already drops env, expandenv,
// getHostByName and now, under teamster's own helpers (ADR 0091).
func funcs() template.FuncMap {
	fm := sprig.HermeticTxtFuncMap()
	for _, name := range leftOut {
		delete(fm, name)
	}
	for name, fn := range helpers() {
		fm[name] = fn
	}
	return fm
}

// helpers win over sprig: stored templates call default with teamster's
// (value, fallback) order, the reverse of sprig's.
func helpers() template.FuncMap {
	return template.FuncMap{
		"toJSON": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		// Both parameters are any because a missing key of a nil map arrives as
		// an invalid value, which a string parameter rejects outright. That is
		// exactly the case a fallback exists for — and it is just as likely to
		// be the fallback itself, as in `default .Annotations.summary
		// .Labels.alertname`.
		"default": func(value, fallback any) string {
			if text, ok := value.(string); ok && text != "" {
				return text
			}
			text, _ := fallback.(string)
			return text
		},
	}
}
