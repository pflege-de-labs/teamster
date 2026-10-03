package httpserver

import (
	"maps"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// personalEvent is ev as one person's chat sees it: naming only them, and of
// an Alertmanager group only the alerts that name them or nobody, so no
// template can show a person who else was sent the message (ADR 0086).
// addresses are what the event named them by, beyond their id, UPN and mail.
func personalEvent(ev models.Event, person templates.Person, addresses []string) models.Event {
	names := map[string]bool{}
	for _, name := range append([]string{person.ID, person.UPN, person.Mail}, addresses...) {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			names[name] = true
		}
	}

	ev.Labels = ownRecipientLabel(ev.Labels, names)
	if u := ev.Universal; u != nil {
		own := *u
		own.Recipients = named(u.Recipients, names)
		ev.Universal = &own
	}
	if am := ev.Alertmanager; am != nil {
		ev.Alertmanager = ownAlerts(am, names)
	}
	return ev
}

// ownAlerts keeps the alerts addressed to the person or to nobody.
func ownAlerts(am *models.AlertmanagerEvent, names map[string]bool) *models.AlertmanagerEvent {
	own := *am
	own.CommonLabels = ownRecipientLabel(am.CommonLabels, names)
	own.Alerts = nil
	for _, alert := range am.Alerts {
		if raw := alert.Labels[models.RecipientLabel]; strings.TrimSpace(raw) != "" &&
			len(named(strings.Split(raw, ","), names)) == 0 {
			continue
		}
		alert.Labels = ownRecipientLabel(alert.Labels, names)
		own.Alerts = append(own.Alerts, alert)
	}
	// The flat fields describe a group of one alert, which the person's may now be.
	own.Annotations, own.StartsAt, own.EndsAt, own.GeneratorURL = nil, time.Time{}, time.Time{}, ""
	if len(own.Alerts) == 1 {
		alert := own.Alerts[0]
		own.Annotations, own.StartsAt, own.EndsAt, own.GeneratorURL = alert.Annotations, alert.StartsAt, alert.EndsAt, alert.GeneratorURL
	}
	return &own
}

// ownRecipientLabel narrows the recipient label to the person, or drops it
// when it names only others.
func ownRecipientLabel(labels map[string]string, names map[string]bool) map[string]string {
	raw, ok := labels[models.RecipientLabel]
	if !ok {
		return labels
	}
	out := maps.Clone(labels)
	if kept := named(strings.Split(raw, ","), names); len(kept) > 0 {
		out[models.RecipientLabel] = strings.Join(kept, ",")
	} else {
		delete(out, models.RecipientLabel)
	}
	return out
}

// named is the entries of list that name the person.
func named(list []string, names map[string]bool) []string {
	var kept []string
	for _, entry := range list {
		if entry = strings.TrimSpace(entry); names[strings.ToLower(entry)] {
			kept = append(kept, entry)
		}
	}
	return kept
}
