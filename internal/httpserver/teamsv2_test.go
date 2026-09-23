package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

const (
	testWebhookToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	adaptiveCardBody = `{"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":{"type":"AdaptiveCard"}}]}`
)

// teamsV2Server seeds one endpoint, "platform/alerts", pointing at a
// destination the fake messenger will accept.
func teamsV2Server(t *testing.T, msg *fakeMessenger) (*fakeStore, http.Handler) {
	t.Helper()

	st := newFakeStore()
	st.destinations["dest"] = models.Destination{ID: "dest", Name: "Alerts", TeamID: "team", ChannelID: "channel"}
	st.webhooks["hook"] = models.WebhookEndpoint{
		ID:            "hook",
		TeamSlug:      "platform",
		ChannelSlug:   "alerts",
		DestinationID: "dest",
		TokenHash:     hashToken(testWebhookToken),
	}

	return st, newTestServer(t, st, msg).Handler
}

func postTeamsV2(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestTeamsV2PostsToTheConfiguredChannel(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "posted"}
	_, handler := teamsV2Server(t, msg)

	rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken, adaptiveCardBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(msg.posts))
	}
	post := msg.posts[0]
	if post.teamID != "team" || post.channelID != "channel" {
		t.Errorf("posted to %s/%s, want team/channel", post.teamID, post.channelID)
	}
	if cardOf(post.msg) != `{"type":"AdaptiveCard"}` {
		t.Errorf("card = %s, want the attachment forwarded unchanged", cardOf(post.msg))
	}
	if len(post.msg.Cards) != 2 || !strings.Contains(string(post.msg.Cards[1]), "No template is defined") {
		t.Errorf("cards = %s, want the attachment followed by the hint card", post.msg.Cards)
	}
}

// An endpoint naming a template renders it against the parsed payload and the
// raw body, and sends no hint.
func TestTeamsV2RendersTheEndpointsTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		template   *models.Template
		fail       string
		wantStatus int
		wantTitle  string
		wantCard   string
	}{
		{
			name: "a template",
			template: &models.Template{
				ID: "tmpl", Title: "{{ .Payload.title }} ({{ .Alert.Source }})",
				Body: `{"type":"AdaptiveCard","body":[{"type":"TextBlock","text":{{ toJSON .Payload.themeColor }}}]}`,
			},
			wantStatus: http.StatusOK, wantTitle: "Build failed (teamsv2)",
			wantCard: `{"type":"AdaptiveCard","body":[{"type":"TextBlock","text":"FF0000"}]}`,
		},
		{name: "a template that is gone", wantStatus: http.StatusBadGateway},
		{
			name:       "a template that does not render",
			template:   &models.Template{ID: "tmpl", Body: `not json`},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "posted"}
			st, handler := teamsV2Server(t, msg)
			hook := st.webhooks["hook"]
			hook.TemplateID = "tmpl"
			st.webhooks["hook"] = hook
			if tt.template != nil {
				st.templates["tmpl"] = *tt.template
			}

			rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken,
				`{"@type":"MessageCard","themeColor":"FF0000","title":"Build failed","text":"branch main"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != http.StatusOK {
				if len(msg.posts) != 0 {
					t.Errorf("posts = %d, want none", len(msg.posts))
				}
				return
			}
			if len(msg.posts) != 1 {
				t.Fatalf("posts = %d, want 1", len(msg.posts))
			}
			posted := msg.posts[0].msg
			if posted.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", posted.Title, tt.wantTitle)
			}
			if len(posted.Cards) != 1 || string(posted.Cards[0]) != tt.wantCard {
				t.Errorf("cards = %s, want only %s", posted.Cards, tt.wantCard)
			}
		})
	}
}

func TestTeamsV2AcceptsEveryShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantCards int
		wantTitle string
	}{
		// Each shape's own cards, then the hint that no template rendered them.
		{name: "v2 envelope", body: adaptiveCardBody, wantCards: 2},
		{name: "plain text", body: `{"text":"something broke"}`, wantCards: 1},
		{
			name:      "legacy message card",
			body:      `{"@type":"MessageCard","themeColor":"FF0000","title":"Build failed","text":"branch main"}`,
			wantCards: 2,
			wantTitle: "Build failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "posted"}
			_, handler := teamsV2Server(t, msg)

			rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken, tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			if len(msg.posts) != 1 {
				t.Fatalf("posts = %d, want 1", len(msg.posts))
			}
			if got := len(msg.posts[0].msg.Cards); got != tt.wantCards {
				t.Errorf("cards = %d, want %d", got, tt.wantCards)
			}
			if got := msg.posts[0].msg.Title; got != tt.wantTitle {
				t.Errorf("title = %q, want %q", got, tt.wantTitle)
			}
		})
	}
}

func TestTeamsV2SlugsAreCaseInsensitive(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "posted"}
	_, handler := teamsV2Server(t, msg)

	rec := postTeamsV2(t, handler, "/teamsv2/PLATFORM/Alerts/"+testWebhookToken, adaptiveCardBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestTeamsV2Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		failOn     string
		wantStatus int
		wantError  string
	}{
		{
			name:       "unconfigured channel",
			path:       "/teamsv2/platform/nowhere/" + testWebhookToken,
			body:       adaptiveCardBody,
			wantStatus: http.StatusNotFound,
			wantError:  "unknown endpoint",
		},
		{
			name:       "unconfigured team",
			path:       "/teamsv2/nobody/alerts/" + testWebhookToken,
			body:       adaptiveCardBody,
			wantStatus: http.StatusNotFound,
			wantError:  "unknown endpoint",
		},
		{
			name:       "a segment short of an endpoint",
			path:       "/teamsv2/platform/alerts",
			body:       adaptiveCardBody,
			wantStatus: http.StatusNotFound,
			wantError:  "unknown endpoint",
		},
		{
			name:       "a slug no endpoint could have",
			path:       "/teamsv2/Platform!/alerts/" + testWebhookToken,
			body:       adaptiveCardBody,
			wantStatus: http.StatusNotFound,
			wantError:  "unknown endpoint",
		},
		{
			name:       "wrong token",
			path:       "/teamsv2/platform/alerts/wrong",
			body:       adaptiveCardBody,
			wantStatus: http.StatusUnauthorized,
			wantError:  "invalid token",
		},
		{
			name:       "not JSON",
			path:       "/teamsv2/platform/alerts/" + testWebhookToken,
			body:       "{",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid JSON",
		},
		{
			name:       "nothing to send",
			path:       "/teamsv2/platform/alerts/" + testWebhookToken,
			body:       `{"type":"message"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "neither text nor an adaptive card",
		},
		{
			name:       "the destination went away",
			path:       "/teamsv2/platform/alerts/" + testWebhookToken,
			body:       adaptiveCardBody,
			failOn:     "GetDestination",
			wantStatus: http.StatusBadGateway,
			wantError:  "destination",
		},
		{
			name:       "the store is down",
			path:       "/teamsv2/platform/alerts/" + testWebhookToken,
			body:       adaptiveCardBody,
			failOn:     "GetWebhookEndpointBySlug",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "not a POST",
			method:     http.MethodGet,
			path:       "/teamsv2/platform/alerts/" + testWebhookToken,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "not a POST, and not an endpoint either",
			method:     http.MethodGet,
			path:       "/teamsv2/",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "posted"}
			st, handler := teamsV2Server(t, msg)
			if tt.failOn != "" {
				st.fail(tt.failOn)
			}

			method := tt.method
			if method == "" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantError != "" && !strings.Contains(rec.Body.String(), tt.wantError) {
				t.Errorf("body = %s, want it to mention %q", rec.Body, tt.wantError)
			}
			if len(msg.posts) != 0 {
				t.Errorf("posts = %d, want none", len(msg.posts))
			}
		})
	}
}

func TestTeamsV2RejectsAnOversizedBody(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "posted"}
	_, handler := teamsV2Server(t, msg)

	body := `{"text":"` + strings.Repeat("a", maxTeamsV2Bytes+1) + `"}`
	rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body)
	}
	if len(msg.posts) != 0 {
		t.Errorf("posts = %d, want none", len(msg.posts))
	}
}

func TestTeamsV2ReportsAGraphFailure(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{postErr: errStore}
	_, handler := teamsV2Server(t, msg)

	rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken, adaptiveCardBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the JSON error shape: %v", err)
	}
	if body["error"] == "" {
		t.Errorf("body = %v, want an error", body)
	}
}

func TestTeamsV2NeedsNoWebhookToken(t *testing.T) {
	t.Parallel()

	// The path carries the secret. A sender that could set X-Teamster-Token
	// would not have needed this endpoint in the first place.
	msg := &fakeMessenger{messageID: "posted"}
	_, handler := teamsV2Server(t, msg)

	req := httptest.NewRequest(http.MethodPost, "/teamsv2/platform/alerts/"+testWebhookToken, strings.NewReader(adaptiveCardBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}
