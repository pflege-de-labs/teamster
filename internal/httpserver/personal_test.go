package httpserver

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func TestPersonalEvent(t *testing.T) {
	t.Parallel()

	alice := templates.Person{ID: "oid-alice", UPN: "alice@corp.example", Mail: "a.a@corp.example"}
	start := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	alert := func(fingerprint, recipients string) models.AlertmanagerAlert {
		labels := map[string]string{"alertname": "Disk"}
		if recipients != "" {
			labels[models.RecipientLabel] = recipients
		}
		return models.AlertmanagerAlert{
			Fingerprint: fingerprint, Labels: labels, StartsAt: start,
			Annotations: map[string]string{"summary": fingerprint}, GeneratorURL: "https://prom/" + fingerprint,
		}
	}

	tests := []struct {
		name      string
		ev        models.Event
		addresses []string
		// want is the person's event, printed by describe.
		want string
	}{
		{
			name: "the list names only them",
			ev: models.Event{Universal: &models.UniversalEvent{
				Recipients: []string{"bob@corp.example", "ALICE@corp.example", "carol@corp.example"},
			}},
			want: "labels=map[] recipients=[ALICE@corp.example]",
		},
		{
			name:      "an alias they were named by counts",
			ev:        models.Event{Labels: map[string]string{models.RecipientLabel: "bob@corp.example, alias@corp.example"}},
			addresses: []string{"alias@corp.example"},
			want:      "labels=map[teamster_recipient:alias@corp.example]",
		},
		{
			name: "a label naming only others is dropped",
			ev:   models.Event{Labels: map[string]string{"kind": "pw", models.RecipientLabel: "bob@corp.example"}},
			want: "labels=map[kind:pw]",
		},
		{
			name: "an event naming nobody is left alone",
			ev:   models.Event{Labels: map[string]string{"kind": "pw"}, Universal: &models.UniversalEvent{URL: "u"}},
			want: "labels=map[kind:pw] recipients=[]",
		},
		{
			name: "a group keeps their alerts and the unaddressed ones",
			ev: models.Event{
				Labels: map[string]string{models.RecipientLabel: "alice@corp.example,bob@corp.example"},
				Alertmanager: &models.AlertmanagerEvent{Alerts: []models.AlertmanagerAlert{
					alert("a", "oid-alice,bob@corp.example"), alert("b", "bob@corp.example"), alert("c", ""),
				}},
			},
			want: "labels=map[teamster_recipient:alice@corp.example] alerts=[a:oid-alice c:] flat=",
		},
		{
			name: "a group cut to one alert fills the flat fields",
			ev: models.Event{Alertmanager: &models.AlertmanagerEvent{
				Alerts:            []models.AlertmanagerAlert{alert("a", "a.a@corp.example"), alert("b", "bob@corp.example")},
				CommonLabels:      map[string]string{"alertname": "Disk"},
				CommonAnnotations: map[string]string{},
			}},
			want: "labels=map[] alerts=[a:a.a@corp.example] flat=a",
		},
		{
			name: "a group with none of theirs is empty",
			ev: models.Event{Alertmanager: &models.AlertmanagerEvent{
				Alerts:      []models.AlertmanagerAlert{alert("b", "bob@corp.example")},
				Annotations: map[string]string{"summary": "b"},
			}},
			want: "labels=map[] alerts=[] flat=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := describe(tt.ev)
			got := personalEvent(tt.ev, alice, tt.addresses)
			if d := describe(got); d != tt.want {
				t.Errorf("personalEvent() = %s, want %s", d, tt.want)
			}
			// Every other person's copy starts from the same event.
			if after := describe(tt.ev); after != before {
				t.Errorf("the event changed under personalEvent: %s, was %s", after, before)
			}
		})
	}
}

// describe prints what personalEvent may change.
func describe(ev models.Event) string {
	out := fmt.Sprintf("labels=%v", ev.Labels)
	if u := ev.Universal; u != nil {
		out += fmt.Sprintf(" recipients=%v", u.Recipients)
	}
	if am := ev.Alertmanager; am != nil {
		var alerts []string
		for _, a := range am.Alerts {
			alerts = append(alerts, a.Fingerprint+":"+a.Labels[models.RecipientLabel])
		}
		out += fmt.Sprintf(" alerts=%v flat=%s", alerts, am.Annotations["summary"])
		if len(am.Alerts) != 1 && (!am.StartsAt.IsZero() || am.GeneratorURL != "") {
			out += " (stale flat fields)"
		}
	}
	return out
}

func TestPersonalMessagesNameOnlyTheirRecipient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		template string
		body     string
		want     []string
	}{
		{
			name:     "universal",
			path:     "/webhook/universal",
			template: `{{ .Recipient.GivenName }} {{ .Recipient.Department }} {{ .Recipient.Address.City }}:{{ range .Event.Universal.Recipients }} {{ . }}{{ end }}`,
			body:     `{"labels":{"kind":"password"},"recipients":["alice@corp.example","b.b@corp.example"]}`,
			// The chat's Markdown escapes the dots.
			want: []string{
				`a:alice: Alice Platform Berlin: alice@corp\.example`,
				`a:bob: Bob  : b\.b@corp\.example`,
			},
		},
		{
			name:     "alertmanager",
			path:     "/webhook/alertmanager",
			template: `{{ .Recipient.GivenName }}:{{ range .Event.Alertmanager.Alerts }} {{ .Fingerprint }}{{ end }}`,
			body: `{"status":"firing","groupKey":"g","alerts":[
				{"status":"firing","labels":{"kind":"password","teamster_recipient":"alice@corp.example"},"fingerprint":"a"},
				{"status":"firing","labels":{"kind":"password","teamster_recipient":"bob@corp.example"},"fingerprint":"b"},
				{"status":"firing","labels":{"kind":"password"},"fingerprint":"c"}
			]}`,
			want: []string{"a:alice: Alice: a c", "a:bob: Bob: b c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, nil)
			reminder := f.store.templates["reminder"]
			reminder.Text = tt.template
			f.store.templates["reminder"] = reminder
			alice := f.store.directory["oid-alice"]
			alice.Profile = models.Profile{Department: "Platform", Address: models.Address{City: "Berlin"}}
			f.store.directory["oid-alice"] = alice

			rec := postWebhook(t, f.handler, tt.path, senderToken, tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
			}
			if got := f.sentTo(); strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("sent = %q, want %q", got, tt.want)
			}
		})
	}
}
