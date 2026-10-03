---
title: Metrics
weight: 5
---

Every metric Teamster exports, its type and its attributes. Metrics are off unless
`metrics.enabled` is set; see [Configuration](../configuration/#metrics).

## Names

Instruments are named the OpenTelemetry way. The Prometheus exporter turns each name into
underscores and adds unit and type suffixes.

| OpenTelemetry | Prometheus |
| --- | --- |
| `teamster.deliveries` (counter) | `teamster_deliveries_total` |
| `teamster.active_events` (gauge) | `teamster_active_events` |
| `http.server.request.duration` (histogram, seconds) | `http_server_request_duration_seconds` |

Attribute names keep dots over OTLP and use underscores in Prometheus (`http.route` becomes
`http_route`).

## Teamster metrics

| Metric | Prometheus name | Type | Unit | Attributes | Description |
| --- | --- | --- | --- | --- | --- |
| `teamster.deliveries` | `teamster_deliveries_total` | counter | `{delivery}` | `route`, `outcome` | Messages delivered to a channel or a person, by route and outcome. |
| `teamster.webhook.receipts` | `teamster_webhook_receipts_total` | counter | `{message}` | `source`, `state` | Messages received on a webhook, refused ones included. |
| `teamster.render.failures` | `teamster_render_failures_total` | counter | `{failure}` | `template`, `stage` | Messages that could not be rendered. |
| `teamster.active_events` | `teamster_active_events` | gauge | `{card}` | — | Cards currently tracked, one per event per channel. |
| `teamster.destinations.without_app` | `teamster_destinations_without_app` | gauge | `{destination}` | — | Destinations in a team the bot's Teams app is not known to be installed in. |
| `teamster.app.installs` | `teamster_app_installs_total` | counter | `{install}` | `outcome` | Attempts to make the bot's Teams app reach a person. |
| `teamster.directory.lookups` | `teamster_directory_lookups_total` | counter | `{lookup}` | `result` | Addresses resolved to a person, by where the answer came from. |
| `teamster.directory.runs` | `teamster_directory_runs_total` | counter | `{run}` | `kind`, `outcome` | Runs that install the Teams app for the tenant. |
| `teamster.directory.users` | `teamster_directory_users` | gauge | `{person}` | `state` | People in the directory, by install state. |
| `teamster.audit.failed` | `teamster_audit_failed_total` | counter | `{event}` | `sink` | Audit events a sink refused. |
| `teamster.audit.dropped` | `teamster_audit_dropped_total` | counter | `{event}` | `sink` | Audit events dropped because a sink's queue was full. |
| `teamster.throttled` | `teamster_throttled_total` | counter | `{call}` | `api` | Calls Microsoft Graph or the Bot Connector refused with `429` or `503`, by API. Every refused attempt counts. |
| `teamster.pacing.wait` | `teamster_pacing_wait_seconds` | histogram | `s` | — | Time a Bot Connector call waited for the bot's budget. See [Pace the calls to Teams](../../guides/teams-bot/#pace-the-calls-to-teams). |

A counter appears in the output once it has counted something. The gauges are read from the
database when collected, and each answer is reused for one second.

## HTTP metrics

Recorded by the OpenTelemetry HTTP instrumentation, with its standard attributes.

| Metric | Prometheus name | Type | Description |
| --- | --- | --- | --- |
| `http.server.request.duration` | `http_server_request_duration_seconds` | histogram | Time to answer a request on `server.addr`, by `http.route`, method and status. `/healthz` and `/readyz` are not recorded. |
| `http.client.request.duration` | `http_client_request_duration_seconds` | histogram | Time Microsoft Graph and the Bot Connector took, by server address, method and status. |

Histograms are exponential, exported to Prometheus as native histograms. Request and response body
size histograms are dropped.

## Attribute values

| Metric | Attribute | Values |
| --- | --- | --- |
| `teamster.deliveries` | `route` | The route's name, or its id when it has none. For Teams V2, `teamsv2:<team>/<channel>`. |
| `teamster.deliveries` | `outcome` | `posted`, `updated`, `failed`, `blocked`, `app_missing`; for a person who cannot be reached, `invalid-address`, `unknown-recipient`, `ineligible`, `not-installed`, `no-recipient` |
| `teamster.webhook.receipts` | `source` | `alertmanager`, `universal`, `teamsv2`, `bot` |
| `teamster.webhook.receipts` | `state` | `alertmanager`, `universal`: `open`, `closed`, empty for no state, `refused` (unknown or missing token), `forbidden` (token scope). `teamsv2`: `accepted`, `refused`, `unknown`, `rejected`, `error`. `bot`: `accepted`, or the reason it was refused or ignored. |
| `teamster.render.failures` | `template` | Template id; empty when none was found. |
| `teamster.render.failures` | `stage` | `template`, `destination`, `recipient`, `render` |
| `teamster.app.installs` | `outcome` | `installed`, `already`, `failed`, `ineligible` |
| `teamster.directory.lookups` | `result` | `store`, `graph`, `negative-cache`, `unknown` |
| `teamster.directory.runs` | `kind` | `manual`, `periodic` |
| `teamster.directory.runs` | `outcome` | `done`, `failed`, `lost` |
| `teamster.directory.users` | `state` | `unknown`, `installed`, `removed`, `failed`, `ineligible`, `departed` |
| `teamster.audit.failed`, `teamster.audit.dropped` | `sink` | `database`, `file`, `nats` |
| `teamster.throttled` | `api` | `bot`, `graph` |

`app_missing` is a channel post the Bot Connector refused, almost always because the Teams app is
not installed in that team. Like `blocked`, it lasts until somebody acts.

## Resource

| Attribute | Value |
| --- | --- |
| `service.name` | `metrics.service-name`, default `teamster` |

In Prometheus the resource appears as `target_info`.

## Limits

| Limit | Value |
| --- | --- |
| Series per metric | 2000. Further attribute combinations are aggregated into one overflow series. |

## See also

* [Monitor Teamster](../../guides/observability/)
* [Configuration: metrics](../configuration/#metrics)
* [Helm values: metrics](../helm-values/#metrics)
