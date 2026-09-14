package metrics

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// gaugeTTL is how long an observed count is reused. Short enough that the
// number still describes now, long enough that scraping in a loop cannot turn
// into a query in a loop.
const gaugeTTL = time.Second

// Delivery outcomes. A card is either new in its channel or an edit of one that
// is already there, and telling them apart is how "nothing is being delivered"
// is distinguished from "nothing new is happening".
const (
	OutcomePosted  = "posted"
	OutcomeUpdated = "updated"
	OutcomeFailed  = "failed"
)

// Rendering stages, so a failure says which lookup or which template broke
// rather than only that the alert was rejected.
const (
	StageTemplate    = "template"
	StageDestination = "destination"
	StageRender      = "render"
)

func (m *Metrics) instruments() error {
	meter := m.provider.Meter(scope)

	var errs []error
	var err error

	m.deliveries, err = meter.Int64Counter(
		"teamster.deliveries",
		metric.WithDescription("Messages delivered to a channel, by route and outcome."),
		metric.WithUnit("{delivery}"),
	)
	errs = append(errs, err)

	m.receipts, err = meter.Int64Counter(
		"teamster.webhook.receipts",
		metric.WithDescription("Alerts received on a webhook, by source and status."),
		metric.WithUnit("{alert}"),
	)
	errs = append(errs, err)

	m.renderFails, err = meter.Int64Counter(
		"teamster.render.failures",
		metric.WithDescription("Alerts that could not be rendered, by template and stage."),
		metric.WithUnit("{failure}"),
	)
	errs = append(errs, err)

	m.activeAlerts, err = meter.Int64ObservableGauge(
		"teamster.active_alerts",
		metric.WithDescription("Cards currently tracked, one per alert per channel."),
		metric.WithUnit("{card}"),
	)
	errs = append(errs, err)

	return errors.Join(errs...)
}

// DeliveryRecorded counts one message on its way to a channel. The route is the
// name an operator gave it, which is what they will look for when one channel
// stops receiving and the rest carry on.
func (m *Metrics) DeliveryRecorded(ctx context.Context, route, outcome string) {
	m.deliveries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("route", route),
		attribute.String("outcome", outcome),
	))
}

// WebhookReceived counts an alert as it arrives, including the ones refused for
// a bad token — which are otherwise visible only as a 401 nobody is watching.
func (m *Metrics) WebhookReceived(ctx context.Context, source, status string) {
	m.receipts.Add(ctx, 1, metric.WithAttributes(
		attribute.String("source", source),
		attribute.String("status", status),
	))
}

// RenderFailed counts an alert that never became a message. Today these surface
// only as a 502 to whoever sent the webhook.
func (m *Metrics) RenderFailed(ctx context.Context, templateID, stage string) {
	m.renderFails.Add(ctx, 1, metric.WithAttributes(
		attribute.String("template", templateID),
		attribute.String("stage", stage),
	))
}

// ObserveActiveAlerts asks the store how many cards are open, once per
// collection. The callback runs on the collecting goroutine, so it has to be a
// count and not a scan.
func (m *Metrics) ObserveActiveAlerts(count func(context.Context) (int64, error)) error {
	if !m.enabled {
		return nil
	}

	// Registering twice would leave the first callback running against whatever
	// it closed over, which for this gauge is a database somebody may be about
	// to close.
	if m.registration != nil {
		if err := m.registration.Unregister(); err != nil {
			return err
		}
		m.registration = nil
	}

	// The callback runs on the collecting goroutine, once per scrape and once
	// per OTLP interval, and the listener it serves is unauthenticated. The
	// answer is cached for a moment so a burst of scrapes is one query rather
	// than one each.
	count = cached(count, gaugeTTL)

	registration, err := m.provider.Meter(scope).RegisterCallback(
		func(ctx context.Context, observer metric.Observer) error {
			open, err := count(ctx)
			if err != nil {
				return err
			}
			observer.ObserveInt64(m.activeAlerts, open)
			return nil
		},
		m.activeAlerts,
	)
	if err != nil {
		return err
	}

	m.registration = registration
	return nil
}

// cached remembers the last answer for a moment. The metrics listener takes no
// credentials, so the rate at which it can be made to ask the database is
// whatever an unauthenticated caller chooses; this makes that rate a property
// of the clock instead.
func cached(count func(context.Context) (int64, error), ttl time.Duration) func(context.Context) (int64, error) {
	var (
		mu    sync.Mutex
		value int64
		taken time.Time
		known bool
	)

	return func(ctx context.Context) (int64, error) {
		mu.Lock()
		defer mu.Unlock()

		if known && time.Since(taken) < ttl {
			return value, nil
		}

		fresh, err := count(ctx)
		if err != nil {
			return 0, err
		}
		value, taken, known = fresh, time.Now(), true
		return value, nil
	}
}
