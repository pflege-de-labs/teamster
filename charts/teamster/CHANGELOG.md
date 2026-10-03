# Chart changelog

Notable changes to the teamster Helm chart, one entry per release tag `teamster-X.Y.Z`. Changes
to the application itself are in the [service changelog](../../CHANGELOG.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

* `values.yaml` lists `config.settings.bot.pacing` commented out with its defaults, and says to
  divide the tenant's budget by `replicaCount`.

## [0.11.0] — 2026-10-02

App version 0.12.0: naming people in a message takes permission (**breaking** for senders whose
token has no message level), broadcasts to everyone the bot can reach, and an Alertmanager
notification is one card per alert group again; see the [service changelog](../../CHANGELOG.md).

### Added

* `values.yaml` lists the remaining settings commented out with their defaults:
  `config.settings.database.migrate` and `.connect-timeout`, the sovereign-cloud endpoints
  `graph.token-url` and `.scope` and `bot.token-url`, `.scope` and `.metadata-url`, and
  `ui.locale-dir`.

### Changed

* The comment on `config.settings.auth.object-id-claim` says it also identifies the creator of a
  token that may message only them.

## [0.10.0] — 2026-10-01

App version 0.11.0: an audit trail with NATS export, local groups, record ownership and sharing,
webhook permissions and self-service scoped tokens; see the [service changelog](../../CHANGELOG.md).

### Changed

* The comment on `config.settings.auth.groups-claim` says what the claim is now used for: local
  groups can name its values.

### Added

* `config.settings.audit` documents the audit trail's settings. `file: "-"` sends the JSON lines to
  the pod's stdout, for the cluster's log pipeline. `audit.nats` publishes to NATS JetStream. A URL
  with credentials goes in `credentials.extra.TEAMSTER_AUDIT_NATS_URL`. `audit.nats.backfill`
  catches up from the database after NATS was unreachable.
* `nack` declares the audit stream, durable consumers and an `Account` as
  [NACK](https://github.com/nats-io/nack) resources. The stream's name and subjects follow
  `audit.nats`, and it keeps its history on uninstall. NACK's CRDs and controller have to be
  installed and configured separately. The chart refuses `nack.stream.enabled` with
  `audit.nats.create-stream` ([ADR 0079](../../docs/adr/0079-the-chart-declares-the-audit-stream-through-nack.md)).
* `values.yaml` lists every audit setting commented out, with its default, and shows how to mount
  a NATS creds file.

## [0.9.0] — 2026-09-30

App version 0.10.0: the admin UI names the zone of every time and can show them in the browser's
zone, and a manual run retries failed installs at once; see the
[service changelog](../../CHANGELOG.md).

## [0.8.1] — 2026-09-30

App version 0.9.1, which fixes global install when a Graph permission is missing; see the
[service changelog](../../CHANGELOG.md).

## [0.8.0] — 2026-09-30

App version 0.9.0, which is a breaking release: the universal webhook's fields, the template data
and the schema changed; see the [service changelog](../../CHANGELOG.md).

### Added

* The global-install keys under `config.settings.bot`, `config.settings.webhook`, and
  `config.settings.auth.object-id-claim` are documented in `values.yaml`.

### Changed

* The README's install example configures the bot and no longer asks for a webhook token, and new
  sections cover the bot and messages to individual people.

## [0.7.1] — 2026-09-29

App version 0.8.0.

### Fixed

* The headless Service is rendered when the StatefulSet shape is derived from
  `database.driver` rather than set by hand.

## [0.7.0] — 2026-09-29

App version 0.8.0.

### Added

* `commonLabels`, and `labels` on each component, for every resource the chart renders; see
  [Labels](README.md#labels).
* `config.settings.log` documented in `values.yaml`.

## [0.6.0] — 2026-09-28

App version 0.7.0.

### Changed

* The bot configuration is required for any delivery: channel cards are posted through the bot
  from app 0.7.0 on. `config.settings.bot.service-url` is documented.
* `credentials.webhookToken` is optional; tokens issued at `/admin/tokens` work without it.

## [0.5.0] — 2026-09-23

App version 0.6.0.

### Added

* `config.settings.auth.broker` and `credentials.brokerTokenEncryptionKey` for the delegated
  Teams/Channels picker.
* `config.settings.samples` for editor completion.
* `config.settings.server.external-url` for the link in the built-in message.

## [0.4.0] — 2026-09-23

App version 0.5.0.

## [0.3.0] — 2026-09-21

App version 0.3.0. Postgres needs app 0.4.0 and the bot app 0.5.0; the chart shipped ahead.

### Added

* `config.settings.bot` and `credentials.botClientSecret` for the Teams bot.
* `httpRoute.external` and `httpRoute.internal`, so the webhooks and the admin UI can sit on
  different Gateway listeners ([ADR 0033](../../docs/adr/0033-split-httproute-external-and-internal.md)).
* Postgres: a Deployment with any replica count and a disruption budget for `postgres`, a
  StatefulSet for `sqlite`; see [Choosing a backend](README.md#choosing-a-backend)
  ([ADR 0023](../../docs/adr/0023-chart-deploys-either-shape.md)).

### Changed

* Updated the busybox image.

## [0.2.0] — 2026-09-14

App version 0.3.0.

### Added

* Metrics: `config.settings.metrics.enabled` publishes the metrics port and can render a
  ServiceMonitor, or pushes over OTLP; see [Metrics](README.md#metrics).

## [0.1.0] — 2026-09-11

App version 0.2.0. First release: teamster with SQLite as a StatefulSet or Deployment, a Service,
an Ingress or HTTPRoute, configuration and credentials from values, `extraObjects`, probes with
an optional startupProbe, and `helm test`
([ADR 0016](../../docs/adr/0016-helm-chart.md)).

[Unreleased]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.11.0...HEAD
[0.11.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.10.0...teamster-0.11.0
[0.10.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.9.0...teamster-0.10.0
[0.9.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.8.1...teamster-0.9.0
[0.8.1]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.8.0...teamster-0.8.1
[0.8.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.7.1...teamster-0.8.0
[0.7.1]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.7.0...teamster-0.7.1
[0.7.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.6.0...teamster-0.7.0
[0.6.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.5.0...teamster-0.6.0
[0.5.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.4.0...teamster-0.5.0
[0.4.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.3.0...teamster-0.4.0
[0.3.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.2.0...teamster-0.3.0
[0.2.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.1.0...teamster-0.2.0
[0.1.0]: https://github.com/pflege-de-labs/teamster/releases/tag/teamster-0.1.0
