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
//
// OutcomeBlocked is its own outcome rather than a failure because it is the
// only one that will not come right on its own: the person uninstalled or
// blocked the bot, so every later attempt fails the same way until somebody
// acts. Counting it with the transient failures would hide that in the noise.
const (
	OutcomePosted  = "posted"
	OutcomeUpdated = "updated"
	OutcomeFailed  = "failed"
	OutcomeBlocked = "blocked"
	// OutcomeAppMissing is a channel whose team lacks the bot's Teams app: like
	// blocked, it lasts until somebody installs it (ADR 0045).
	OutcomeAppMissing = "app_missing"
)

// Rendering stages, so a failure says which lookup or which template broke
// rather than only that the event was rejected.
const (
	StageTemplate    = "template"
	StageDestination = "destination"
	StageRecipient   = "recipient"
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
		metric.WithDescription("Messages received on a webhook, by source and status."),
		metric.WithUnit("{message}"),
	)
	errs = append(errs, err)

	m.renderFails, err = meter.Int64Counter(
		"teamster.render.failures",
		metric.WithDescription("Messages that could not be rendered, by template and stage."),
		metric.WithUnit("{failure}"),
	)
	errs = append(errs, err)

	m.activeEvents, err = meter.Int64ObservableGauge(
		"teamster.active_events",
		metric.WithDescription("Cards currently tracked, one per event per channel."),
		metric.WithUnit("{card}"),
	)
	errs = append(errs, err)

	m.withoutApp, err = meter.Int64ObservableGauge(
		"teamster.destinations.without_app",
		metric.WithDescription("Destinations in a team the bot's Teams app is not known to be installed in."),
		metric.WithUnit("{destination}"),
	)
	errs = append(errs, err)

	m.installs, err = meter.Int64Counter(
		"teamster.app.installs",
		metric.WithDescription("Attempts to make the bot's Teams app reach a person, by outcome."),
		metric.WithUnit("{install}"),
	)
	errs = append(errs, err)

	m.lookups, err = meter.Int64Counter(
		"teamster.directory.lookups",
		metric.WithDescription("Addresses resolved to a person, by where the answer came from."),
		metric.WithUnit("{lookup}"),
	)
	errs = append(errs, err)

	m.reconcileRuns, err = meter.Int64Counter(
		"teamster.directory.runs",
		metric.WithDescription("Runs that install the Teams app for the tenant, by kind and outcome."),
		metric.WithUnit("{run}"),
	)
	errs = append(errs, err)

	m.directoryUsers, err = meter.Int64ObservableGauge(
		"teamster.directory.users",
		metric.WithDescription("People in the directory, by install state."),
		metric.WithUnit("{person}"),
	)
	errs = append(errs, err)

	m.auditFailed, err = meter.Int64Counter(
		"teamster.audit.failed",
		metric.WithDescription("Audit events a sink refused, by sink."),
		metric.WithUnit("{event}"),
	)
	errs = append(errs, err)

	m.auditDropped, err = meter.Int64Counter(
		"teamster.audit.dropped",
		metric.WithDescription("Audit events dropped because a sink's queue was full, by sink."),
		metric.WithUnit("{event}"),
	)
	errs = append(errs, err)

	return errors.Join(errs...)
}

// AuditFailed counts an audit event a sink could not take.
func (m *Metrics) AuditFailed(ctx context.Context, sink string) {
	m.auditFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("sink", sink)))
}

// AuditDropped counts an audit event a full queue turned away.
func (m *Metrics) AuditDropped(ctx context.Context, sink string) {
	m.auditDropped.Add(ctx, 1, metric.WithAttributes(attribute.String("sink", sink)))
}

// AppInstall counts one person the installer handled, never who, so the
// attribute stays small whatever the tenant's size.
func (m *Metrics) AppInstall(ctx context.Context, outcome string) {
	m.installs.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// DirectoryLookup counts a resolved address by source, which shows how much
// Graph the store is saving.
func (m *Metrics) DirectoryLookup(ctx context.Context, result string) {
	m.lookups.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

// ReconcileRun counts a finished install run.
func (m *Metrics) ReconcileRun(ctx context.Context, kind, outcome string) {
	m.reconcileRuns.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("outcome", outcome),
	))
}

// ObserveDirectoryUsers asks the store for people per install state, once per
// collection and cached like the other gauges.
func (m *Metrics) ObserveDirectoryUsers(count func(context.Context) (map[string]int64, error)) error {
	if !m.enabled {
		return nil
	}
	if m.directoryReg != nil {
		if err := m.directoryReg.Unregister(); err != nil {
			return err
		}
		m.directoryReg = nil
	}

	var (
		mu    sync.Mutex
		value map[string]int64
		taken time.Time
	)
	registration, err := m.provider.Meter(scope).RegisterCallback(
		func(ctx context.Context, observer metric.Observer) error {
			mu.Lock()
			defer mu.Unlock()
			if value == nil || time.Since(taken) >= gaugeTTL {
				fresh, err := count(ctx)
				if err != nil {
					return err
				}
				value, taken = fresh, time.Now()
			}
			for state, n := range value {
				observer.ObserveInt64(m.directoryUsers, n, metric.WithAttributes(attribute.String("state", state)))
			}
			return nil
		},
		m.directoryUsers,
	)
	if err != nil {
		return err
	}
	m.directoryReg = registration
	return nil
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

// WebhookReceived counts an event as it arrives, including the ones refused for
// a bad token — which are otherwise visible only as a 401 nobody is watching.
func (m *Metrics) WebhookReceived(ctx context.Context, source, state string) {
	m.receipts.Add(ctx, 1, metric.WithAttributes(
		attribute.String("source", source),
		attribute.String("state", state),
	))
}

// RenderFailed counts an event that never became a message. Today these surface
// only as a 502 to whoever sent the webhook.
func (m *Metrics) RenderFailed(ctx context.Context, templateID, stage string) {
	m.renderFails.Add(ctx, 1, metric.WithAttributes(
		attribute.String("template", templateID),
		attribute.String("stage", stage),
	))
}

// ObserveActiveEvents asks the store how many cards are open, once per
// collection. The callback runs on the collecting goroutine, so it has to be a
// count and not a scan.
func (m *Metrics) ObserveActiveEvents(count func(context.Context) (int64, error)) error {
	return m.observe(m.activeEvents, &m.registration, count)
}

// ObserveDestinationsWithoutApp asks the store how many destinations lack the
// bot's app, once per collection, so a missing install can be alerted on.
func (m *Metrics) ObserveDestinationsWithoutApp(count func(context.Context) (int64, error)) error {
	return m.observe(m.withoutApp, &m.appReg, count)
}

func (m *Metrics) observe(gauge metric.Int64ObservableGauge, current *metric.Registration, count func(context.Context) (int64, error)) error {
	if !m.enabled {
		return nil
	}

	// Registering twice would leave the first callback running against whatever
	// it closed over, which for these gauges is a database somebody may be about
	// to close.
	if *current != nil {
		if err := (*current).Unregister(); err != nil {
			return err
		}
		*current = nil
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
			observer.ObserveInt64(gauge, open)
			return nil
		},
		gauge,
	)
	if err != nil {
		return err
	}

	*current = registration
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
