# Chart changelog

Notable changes to the teamster Helm chart, one entry per release tag `teamster-X.Y.Z`. Changes
to the application itself are in the [service changelog](../../CHANGELOG.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

* The global-install keys under `config.settings.bot` are documented in `values.yaml`.

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

[Unreleased]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.7.1...HEAD
[0.7.1]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.7.0...teamster-0.7.1
[0.7.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.6.0...teamster-0.7.0
[0.6.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.5.0...teamster-0.6.0
[0.5.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.4.0...teamster-0.5.0
[0.4.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.3.0...teamster-0.4.0
[0.3.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.2.0...teamster-0.3.0
[0.2.0]: https://github.com/pflege-de-labs/teamster/compare/teamster-0.1.0...teamster-0.2.0
[0.1.0]: https://github.com/pflege-de-labs/teamster/releases/tag/teamster-0.1.0
