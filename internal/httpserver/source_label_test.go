package httpserver

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// A route can select on the webhook an alert arrived at (ADR 0052), and a
// sender cannot claim another one.
func TestRoutesSelectOnTheSourceLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		body        string
		wantChannel string
	}{
		{
			name: "alertmanager", path: "/webhook/alertmanager",
			body:        `{"alerts":[{"status":"firing","labels":{"alertname":"Disk"},"fingerprint":"fp-am"}]}`,
			wantChannel: "am-channel",
		},
		{
			name: "universal", path: "/webhook/universal",
			body:        `{"status":"firing","labels":{"alertname":"Disk"},"fingerprint":"fp-u"}`,
			wantChannel: "u-channel",
		},
		{
			// The server's value wins over the sender's.
			name: "a universal sender claiming alertmanager", path: "/webhook/universal",
			body:        `{"status":"firing","labels":{"alertname":"Disk","teamster_source":"alertmanager"},"fingerprint":"fp-s"}`,
			wantChannel: "u-channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "graph-1"}
			st, handler := seededServer(t, msg)
			delete(st.routes, "route")
			st.destinations["am"] = models.Destination{ID: "am", TeamID: "team", ChannelID: "am-channel"}
			st.destinations["u"] = models.Destination{ID: "u", TeamID: "team", ChannelID: "u-channel"}
			st.routes["am"] = models.Route{ID: "am", Name: "am", DestinationID: "am", TemplateID: "tmpl",
				LabelSelector: map[string]string{models.SourceLabel: models.SourceAlertmanager}}
			st.routes["u"] = models.Route{ID: "u", Name: "u", DestinationID: "u", TemplateID: "tmpl",
				LabelSelector: map[string]string{models.SourceLabel: models.SourceUniversal}}

			if rec := postWebhook(t, handler, tt.path, "token", tt.body); rec.Code != http.StatusOK {
				t.Fatalf("POST = %d, want 200 (%s)", rec.Code, rec.Body.String())
			}
			if len(msg.posts) != 1 || msg.posts[0].channelID != tt.wantChannel {
				t.Fatalf("posts = %+v, want one to %s", msg.posts, tt.wantChannel)
			}
		})
	}
}

// A fingerprint computed from the labels ignores the source label, so a card
// posted before the label existed is still the one a later resolve finds.
func TestTheSourceLabelLeavesTheFingerprintAlone(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)

	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{"team":"db"}}`); rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	want := hashFingerprint(models.Alert{Source: models.SourceUniversal, Labels: map[string]string{"team": "db"}})
	for key := range st.activeAlerts {
		if !strings.HasPrefix(key, want) {
			t.Errorf("active alert %q, want it keyed by fingerprint %s", key, want)
		}
	}
	if len(st.activeAlerts) != 1 {
		t.Errorf("active alerts = %d, want 1", len(st.activeAlerts))
	}
}
