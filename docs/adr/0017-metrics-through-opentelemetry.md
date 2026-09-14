# 0017. Instrument once against OpenTelemetry, export as native histograms

* Status: Accepted
* Date: 2026-09-14

## Context

The service that routes alerts said nothing about itself. Whether deliveries were failing, how long
Microsoft Graph was taking, how many cards it was keeping up to date — none of it left the process
except as log lines, which nobody aggregates until the morning they need them.

Two export shapes are wanted and neither is optional: most deployments scrape `GET /metrics`, and the
rest push OTLP to a collector. Writing the instrumentation twice to serve both is the outcome worth
avoiding.

## Decision

We will **instrument once against the OpenTelemetry metric API** and attach as many readers as the
configuration asks for: the Prometheus exporter on its own listener, an OTLP exporter, both, or
neither. Neither is the default.

The alternative was `prometheus/client_golang` directly with OTLP bridged from it. That is fewer
dependencies for the common case and more work for the other one, and it makes the Prometheus shape
the real one and OTLP a translation of it. Since both are first-class here, the API that both
exporters read from is the one to record against.

**The two histograms are the semantic-convention ones**, recorded by `otelhttp` rather than by hand:
`http.server.request.duration` for everything the server answers, and `http.client.request.duration`
for everything it calls. A dashboard written against either works without knowing this service
exists. Beside them are four instruments this service alone can explain: deliveries by route and
outcome, webhook receipts by source and status, rendering failures by template and stage, and a gauge
of the cards currently tracked.

**Histograms are aggregated as base-2 exponential**, through one view matched on instrument kind. That
is what makes the Prometheus exporter emit a *native* histogram: it renders only
`metricdata.ExponentialHistogram` through `prometheus.NewConstNativeHistogram`, and anything else
becomes a classic bucket list. Matching on kind rather than on name means a histogram added later is
native without anyone remembering this decision.

**`/metrics` gets its own listener**, unauthenticated, defaulting to loopback. The main server's
catch-all route is behind `requireSession(authorize(…))`, so a `/metrics` mounted there would be
unreachable to every scraper; and the attributes name routes, templates and channels, so the port is
one an operator should have to route deliberately. The whole feature is off by default for the same
reason.

**Nothing touches the OpenTelemetry globals.** The meter provider is injected into the HTTP
instrumentation, the tracer provider and propagator are explicit no-ops, and the Prometheus exporter
registers into a registry of its own rather than `prometheus.DefaultRegisterer` — which is process
state pre-populated with collectors this service did not choose.

## Consequences

* The active-alerts gauge is the only instrument that does work when it is read: it counts rows on
  collection, on a listener that asks for no credentials. Its value is cached for a second so the
  query rate is bounded by the clock rather than by how fast someone scrapes.

An operator can see delivery failures, Graph latency and alert volume without reading logs. The
runtime metrics arrive with the rest when metrics are on, under OpenTelemetry's names
(`go_memory_used_bytes`), not the `go_memstats_*` an existing dashboard expects.

**A scrape that does not negotiate protobuf silently degrades.** The text exposition carries only the
synthetic `+Inf` bucket, the sum and the count, because a native histogram has no classic buckets to
render. Rates and averages keep working and every quantile becomes `NaN` — a dashboard that renders
and says nothing, which is worse than one that breaks. Prometheus needs
`--enable-feature=native-histograms`; the README carries the detection rule for when it does not have
it.

The dependency tree grows by the OpenTelemetry SDK, two exporters and `client_golang`. The exporter,
`otelhttp` and the SDK are separate modules on separate version lines, and the mapping this decision
rests on lives in the exporter: the test that scrapes protobuf and asserts native buckets is what
turns a bad Renovate bump from a silent dashboard regression into a red build.

**The Graph client's transport is instrumented below oauth2's, not around it.** Measuring from the
outside would fold an hourly token refresh into Graph's latency and report an Entra outage as a Graph
failure. A side effect: the token request now shares the client's timeout, which it never had.

`http.client.request.duration` carries no error attribute — `otelhttp` drops the error before building
them, and this client collapses every non-2xx into a string. It is the latency signal; the delivery
counter is the failure signal.

Two counters that will not reconcile: `handleAlertmanager` abandons the rest of a batch when one alert
fails, so receipts can exceed deliveries by however many alerts were left. That is a delivery
behaviour worth changing on its own terms, not as part of instrumenting it.

Route and template attributes come from configuration an operator edits, and a counter lives as long
as the process — delete a route, recreate it, and the old series stays. The cardinality limit bounds
that at 2000 series; a deployment with more routes than that needs to know it exists.
