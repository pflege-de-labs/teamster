---
title: Monitor Teamster
weight: 21
---

Set up logging, export metrics to Prometheus or an OpenTelemetry collector, and wire up the
liveness and readiness probes.

## Configure logging

Teamster logs to stderr.

```yaml
log:
  level: info     # debug, info, warn or error
  format: json    # text to read, json for a log pipeline
```

| Key | Environment variable | Default |
| --- | --- | --- |
| `log.level` | `TEAMSTER_LOG_LEVEL` | `info` |
| `log.format` | `TEAMSTER_LOG_FORMAT` | `text` |

### Find the cause of an error

Every request gets an id, returned as the `X-Request-ID` header and logged as `request_id` on every
line of that request. A `5xx` from the API or a webhook answers only
`{"error": "internal server error", "request_id": "…"}`, and a page shows "Something went wrong.
Reference: …". Search the log for that id to find the cause.

A `4xx` still says what to fix. Refused webhook and bot requests are logged as warnings with the
reason. `debug` adds every client error and every accepted bot activity.

## Export metrics

Metrics are off by default. Turn them on, then choose a Prometheus scrape, an OTLP push, or both.

{{< tabs >}}
{{< tab name="Prometheus scrape" >}}

```yaml
metrics:
  enabled: true
  addr: "127.0.0.1:9090"   # its own listener
  path: "/metrics"
  prometheus: true
```

The listener is separate from the admin UI's, is not authenticated, and defaults to loopback,
because the attributes name routes, templates and channels. Bind it to an address your Prometheus
can reach, such as `:9090`, deliberately. It may not share `server.addr`.

{{< /tab >}}
{{< tab name="OTLP push" >}}

```yaml
metrics:
  enabled: true
  prometheus: false
  otlp-endpoint: "otel-collector.monitoring.svc:4318"
  otlp-protocol: "http"    # or grpc
  otlp-interval: "60s"
  otlp-insecure: false     # true sends without TLS
```

`otlp-endpoint` is `host:port`. Push-only opens no listener.

{{< /tab >}}
{{< tab name="Helm" >}}

```yaml
config:
  settings:
    metrics:
      enabled: true

metrics:
  serviceMonitor:
    enabled: true          # needs the Prometheus Operator's CRDs
    labels:
      release: kube-prometheus-stack
```

The chart binds the listener to all interfaces, publishes a `metrics` port on the Service, and
refuses a loopback `addr`. Admit only Prometheus with a NetworkPolicy. For OTLP, set
`otlp-endpoint` under `config.settings.metrics`; push-only renders no port and no ServiceMonitor.
See [Helm values](../../reference/helm-values/).

{{< /tab >}}
{{< /tabs >}}

Every key, with its `TEAMSTER_METRICS_*` variable, is in
[Configuration](../../reference/configuration/). Teamster refuses to start with `enabled: true`,
`prometheus: false` and no `otlp-endpoint`, since nothing would be exported.

### Alert on what needs a person

Every metric is listed in [Metrics](../../reference/metrics/). These are the ones that last until
somebody acts:

| Watch | Means |
| --- | --- |
| `teamster.deliveries` with outcome `app_missing` | The Bot Connector refused a channel post, almost always because the Teams app is not installed in that team. |
| `teamster.destinations.without_app` above zero | A destination is in a team the app is not known to be installed in. |
| `teamster.deliveries` with outcome `failed` | Deliveries are failing. Search the log for the cause. |
| `teamster.webhook.receipts` with state `refused` | Someone is sending a wrong token. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `teamster.audit.failed`, `teamster.audit.dropped` | Audit events were lost. See [Record an audit trail](../audit-trail/). |

In Prometheus, dots become underscores and counters gain `_total`, for example:

```promql
sum by (route) (increase(teamster_deliveries_total{outcome="app_missing"}[15m])) > 0
```

Go runtime metrics use OpenTelemetry's names, such as `go_memory_used_bytes`, not the
`go_memstats_*` an older dashboard may expect.

### Tell throttling from a stuck run

A broadcast or a large message that slows down is either throttled or stuck. Two metrics tell them
apart:

| Watch | Means |
| --- | --- |
| `teamster.throttled` rising | Microsoft refused calls with `429` or `503`. `api` says whether Graph or the Bot Connector did. Teamster waits and retries them. |
| `teamster.pacing.wait` high | Bot Connector calls wait for the bot's budget. The pacing keeps them under the limit. |

```promql
sum by (api) (rate(teamster_throttled_total[5m]))
histogram_quantile(0.95, sum(rate(teamster_pacing_wait_seconds[5m])))
```

If the Bot Connector is throttled often, lower `bot.pacing.rate`; with several replicas, check that
their rates add up to no more than the tenant's budget. See
[Pace the calls to Teams](../teams-bot/#pace-the-calls-to-teams).

### Enable native histograms in Prometheus

The Prometheus exporter emits **native** histograms, which travel only over protobuf. Start
Prometheus with:

```bash
prometheus --enable-feature=native-histograms
```

The chart's ServiceMonitor already asks for `PrometheusProto` first.

{{< callout type="error" >}}
Without the flag, the scrape does not fail. It falls back to text, and every histogram arrives with
only a `+Inf` bucket, the sum and the count. Rates and averages work, `histogram_quantile` returns
`NaN`, and the dashboards render empty.
{{< /callout >}}

This rule fires exactly when that has happened:

```promql
count without(le) (http_server_request_duration_seconds_bucket) == 1
```

To check by hand, compare the two shapes. The text form shows few `_bucket` lines; the protobuf
form is the full histogram:

```bash
curl -s localhost:9090/metrics | grep -c '_bucket'
curl -s -H 'Accept: application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited' \
  localhost:9090/metrics | wc -c
```

## Configure the probes

| Path | Answers `200` when |
| --- | --- |
| `GET /healthz` | The process is running. |
| `GET /readyz` | It can serve: it is not shutting down, and the database answers. |

Both are unauthenticated and answer `HEAD` too. They are served on the main listener,
`server.addr`.

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: http }
readinessProbe:
  httpGet: { path: /readyz, port: http }
```

The Helm chart sets these by default.

* Liveness ignores the database on purpose. Restarting does not bring a database back; it turns an
  outage into a crash loop.
* Readiness ignores Microsoft Graph on purpose. Taking the instance out of rotation when Graph is
  down would close the admin UI exactly when you want to see why delivery fails.
* Readiness fails as soon as a shutdown begins, so a rolling update stops sending traffic before
  the process stops accepting it.

There is no startup probe by default. If the first start after an upgrade runs a long migration,
it can outlast the liveness probe and restart in a loop. The chart's `values.yaml` carries a
`startupProbe` commented out for that case.
