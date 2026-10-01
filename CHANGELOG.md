# Changelog

Notable changes to the Teamster service, one entry per release tag `vX.Y.Z`. The Helm chart is
versioned separately; see [charts/teamster/CHANGELOG.md](charts/teamster/CHANGELOG.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Before 1.0.0 a minor release may break things; those
entries are marked **Breaking**. How each feature works is in the [README](README.md) and the
reasoning behind it in the [ADRs](docs/adr/).

## [Unreleased]

### Added

* The bot answers `help`, `status` and `test` in team channels where the app is installed, when
  it is mentioned. `status` lists the destinations and routes for the channel, and `test` posts a
  test alert there. Upload the app package with a higher `version` to show the channel command
  menu ([ADR 0069](docs/adr/0069-the-bot-answers-commands-in-team-channels.md)).

### Changed

* The bot's commands are named `help`, `status`, `test` and `unlink`, without a slash. Teams shares
  its `/` menu with every app, so `/status` collided with other apps' commands. Typing `/status`
  still works. Upload the app package with a higher `version` to update the command menu
  ([ADR 0068](docs/adr/0068-commands-are-words-addressed-to-the-bot.md)).

### Fixed

* `/test` in a linked chat sends the test alert again instead of answering "Something went wrong".
  It renders with the default template for universal webhooks.

## [0.10.0] — 2026-09-30

### Added

* [Entra permissions and how to minimise them](docs/permissions.md) documents every permission
  Teamster can use, what a leaked secret exposes, and smaller permission profiles. It also records
  why the Graph and bot registrations stay separate
  ([ADR 0066](docs/adr/0066-keep-separate-graph-and-bot-registrations.md)).
* Times in the admin UI name their zone, and the user menu switches them between UTC and the
  browser's zone ([ADR 0067](docs/adr/0067-times-in-the-admin-ui-follow-a-chosen-zone.md)).

### Fixed

* A run started with **Install for all users** retries every failed and ineligible install at once
  instead of waiting out each person's backoff, so granting a permission takes effect with one run.

## [0.9.1] — 2026-09-30

### Fixed

* A global install run refused for a missing permission now names `User.Read.All` in its error,
  and the README explains why `User.ReadBasic.All` is not enough.
* An install Graph refused for a missing `TeamsAppInstallation.*ForUser*` permission stops installs
  for the run instead of marking each person `ineligible`.
* People marked `ineligible` are retried on their backoff, one hour doubling up to a week, so those
  marked by the permission bug above recover once the permission is granted.

## [0.9.0] — 2026-09-30

### Added

* `bot.global-install` installs the bot's Teams app for every enabled member of the tenant, on a
  periodic run shared across replicas
  ([ADR 0059](docs/adr/0059-install-the-teams-app-for-every-member.md)). Four metrics follow it:
  `teamster.app.installs`, `teamster.directory.lookups`, `teamster.directory.runs` and
  `teamster.directory.users`.
* With `bot.global-install` on, nobody can opt out: `/unlink` and the Notifications page's Unlink
  are refused, and removing the app no longer retires the link
  ([ADR 0061](docs/adr/0061-no-opt-out-when-installed-for-everyone.md)). The bot records personal
  installs and removals, and greets a new install with `bot.welcome-message`.
* A route may deliver to the people a message names, picked as **People named in the message** in
  **Delivers to**. Only admins may create one
  ([ADR 0062](docs/adr/0062-a-route-may-deliver-to-the-people-a-message-names.md)).
* A universal message names its people in `recipients`, and any message in the
  `teamster_recipient` label. Each person gets their own message, rendered with `.Recipient`, and
  the answer lists who was not reached: `200 partial`, or `422` when nobody was
  ([ADR 0063](docs/adr/0063-a-message-names-its-recipients.md),
  [ADR 0064](docs/adr/0064-chat-claims-for-people-share-the-recipient-table.md)).
* With `bot.global-install` on, a signed-in user's own Teams chat is found from their Entra object id
  (`auth.object-id-claim`), their UPN or a verified email, instead of a link code; Notifications
  offers **Set up my chat now** when there is no chat yet
  ([ADR 0065](docs/adr/0065-your-own-chat-is-found-from-your-sign-in.md)).
* **People** (`/admin/people`, admins only) shows install progress for the whole tenant and starts
  a run on demand; `GET /api/people/runs/latest` and `POST /api/people/install` are the same over
  the API.
* Each webhook has a default template, used before the built-in message. A new installation is
  seeded with a preset per webhook, and the template editor offers the same presets as starting
  points ([ADR 0055](docs/adr/0055-each-webhook-has-a-default-template.md)).
* The routing graph draws one origin per webhook, feeding the root routes its `teamster_source`
  pin allows, and draws each Teams V2 endpoint straight to its channel. The template graph shows
  each webhook's default template.
* The sidebar footer shows the build version.
* `/admin` shows templates, destinations, webhooks, routes and grants as tabs rather than one long
  page. `?tab=` opens one, and a save lands back on the tab it came from.
* The template preview can show the JSON the bot would send to the Bot Connector, for a channel
  post and for a chat, besides the rendered message
  ([ADR 0058](docs/adr/0058-preview-shows-the-wire-payload.md)).

### Changed

* **Breaking:** what arrives is an event, not an alert
  ([ADR 0056](docs/adr/0056-events-not-alerts.md)). Templates read `.Event` instead of `.Alert`,
  with `State` (`open`/`closed`) and `Key` in place of `Status` and `Fingerprint`. What only one
  webhook knows is under `.Event.Alertmanager` or `.Event.Universal`, and Alertmanager's group
  fields are now available there. Stored templates are rewritten by the migration.
* **Breaking:** `/webhook/universal` takes `key`, `state`, `attributes`, `time` and `url` instead
  of `fingerprint`, `status`, `annotations`, `starts_at` and `generator`; `ends_at` is gone. An
  unknown `state` is refused with 400.
* **Breaking:** the metric `teamster.active_alerts` is now `teamster.active_events`, and the
  `status` attribute of `teamster.webhook.receipts` is now `state`.
* **Breaking:** the schema migration renames the alert tables and cannot be run by the previous
  release.

### Fixed

* The routing picture no longer draws a child that names a person or a channel as delivering to its
  parent's target too; it follows the same rule as routing (ADR 0047).
* The header no longer shifts after the page loads; the sidebar state is applied before first
  paint ([ADR 0054](docs/adr/0054-apply-sidebar-state-before-first-paint.md)).
* A channel post with a card shows its title in the Teams activity feed again, instead of `Card`.
  Every bot message now sends the title, or the first line of its text, as the activity `summary`
  ([ADR 0057](docs/adr/0057-channel-posts-carry-a-feed-summary.md)).

## [0.8.0] — 2026-09-29

### Added

* The bot answers `/help`, `/status`, `/test` and `/unlink` in the personal chat
  ([ADR 0048](docs/adr/0048-bot-answers-commands-in-the-personal-chat.md)).
* Every alert carries a `teamster_source` label (`alertmanager`, `universal` or `teamsv2`), so
  routes can tell the webhooks apart ([ADR 0052](docs/adr/0052-the-receiving-webhook-is-a-label.md)).
* A template lists the webhooks it handles; pickers offer only matching templates, and a
  mismatch at delivery sends the built-in message
  ([ADR 0053](docs/adr/0053-templates-name-the-webhooks-they-handle.md)).
* The global default route can render with a chosen template, set on its row or through
  `GET`/`PUT /api/routes/global-default`, and carried in the export bundle
  ([ADR 0050](docs/adr/0050-catch-all-template-is-a-setting.md)).
* The template preview sits beside the editor.
* `make manifest` builds the Teams app zip, named by version.
* `log.level` and `log.format` (`text` or `json`); every request carries an `X-Request-ID`
  that its log lines repeat.

### Changed

* **Breaking:** a route delivers to a channel *or* a person, chosen in one "Delivers to" field.
  Editors may route only to their own chat; admins to anyone's. A child naming a person no longer
  also posts to its parent's channel. Routes saved with both targets keep delivering to both and
  are badged ([ADR 0047](docs/adr/0047-a-route-targets-a-channel-or-yourself.md)).
* **Breaking:** logging moved to structured `log/slog`. A 5xx answers with the status and request
  id only, no longer the store's or Graph's error text; an import that fails in the store is now
  a 500 rather than a 400
  ([ADR 0046](docs/adr/0046-structured-logging-and-error-boundary.md)).
* A root route needs a target, and a child route that only refines needs a template of its own
  ([ADR 0051](docs/adr/0051-root-routes-need-a-target-refinements-a-template.md)).

### Fixed

* A channel post is one Teams message rather than several
  ([ADR 0049](docs/adr/0049-a-channel-post-is-one-teams-message.md)).
* The request log no longer records the Teams V2 token from the path.
* The app manifest declares `supportsChannelFeatures` for the team scope and refuses a package
  version starting with 0.

## [0.7.0] — 2026-09-28

### Added

* Webhooks accept `Authorization: Bearer`. Admins issue and revoke named tokens at
  `/admin/tokens` or `/api/tokens`; `webhook.token` is optional
  ([ADR 0044](docs/adr/0044-webhook-access-tokens.md)).
* `/admin/teams` shows where the bot's Teams app is installed and what depends on each team. The
  Team picker groups installed and not-installed Teams, and missing installs are marked on
  destinations and counted in metrics.
* Navigation moved to a sidebar, account controls and the language flag to a user menu
  ([ADR 0042](docs/adr/0042-sidebar-navigation-and-user-menu.md)).

### Changed

* **Breaking:** channel cards are posted and edited through the Teams bot, because Microsoft
  Graph refuses app-only channel posts. The bot must be configured and its app installed in each
  team it posts to; Graph is used only to read Teams and channels. The Teams V2 endpoints post
  through the bot too. A card posted by an earlier release is replaced on re-fire
  ([ADR 0045](docs/adr/0045-channel-delivery-through-the-bot.md)).
* `X-Teamster-Token` is deprecated in favour of `Authorization: Bearer`.

## [0.6.0] — 2026-09-23

### Added

* A global default destination catches every message no route claims
  ([ADR 0038](docs/adr/0038-global-default-destination.md)).
* A route with no template sends a built-in message: title and text, the rest of the payload as
  JSON, and a hint card linking to `server.external-url`
  ([ADR 0039](docs/adr/0039-built-in-default-message.md)).
* A Teams V2 endpoint may render through a template
  ([ADR 0040](docs/adr/0040-teams-v2-endpoint-templates.md)).
* "My Teams" in the Destinations picker lists the signed-in admin's own Teams when Keycloak
  brokers the login against Entra (`auth.broker`)
  ([ADR 0037](docs/adr/0037-delegated-teams-via-keycloak-broker-token.md)).
* The template and route editors complete template fields, card properties and the label keys
  and values seen in incoming alerts (`samples.*`)
  ([ADR 0041](docs/adr/0041-editor-completion-from-sampled-labels.md)).

## [0.5.0] — 2026-09-23

### Added

* Alerts in a person's chat: a Teams bot, a linking flow that lets a person opt their own chat in
  and out, routes that deliver to a person, and `/admin/recipients`
  ([ADR 0026](docs/adr/0026-alerts-in-a-persons-chat.md)).
* Uninstalling the bot or sending `unlink`, `stop` or `unsubscribe` retires the link
  ([ADR 0032](docs/adr/0032-retiring-a-link-from-the-chat.md)).
* `POST /teamsv2/{team}/{channel}/{token}` accepts Teams V2 (Power Automate) payloads and posts
  them straight to a channel ([ADR 0030](docs/adr/0030-teams-v2-compatible-webhooks.md)).
* `/webhook/universal` accepts messages without an Alertmanager status; they are delivered once
  and not tracked ([ADR 0035](docs/adr/0035-a-message-without-a-status-is-delivered-once.md)).
* A route without a template sends the payload's own title, text or card
  ([ADR 0036](docs/adr/0036-direct-content-when-a-route-has-no-template.md)).
* `teamster completion` for bash, zsh and fish
  ([ADR 0025](docs/adr/0025-shell-completion.md)).

### Changed

* Every matching root route delivers, not only the highest-priority one; the default route still
  catches only what nothing else matched
  ([ADR 0034](docs/adr/0034-fan-out-across-independent-routes.md)).
* Message text is Markdown ([ADR 0029](docs/adr/0029-templates-are-markdown.md)).

### Fixed

* `teamster migrate` sets `busy_timeout` on SQLite.
* Vendored browser libraries are pinned by version and checksum in `manifest.json`
  ([ADR 0031](docs/adr/0031-vendored-browser-libraries-pinned-and-verified.md)).
* Dependencies updated past GO-2026-5208.

## [0.4.0] — 2026-09-14

### Added

* Postgres as a second store, chosen by `database.driver`, so several instances can share one
  database ([ADR 0022](docs/adr/0022-postgres-second-backend.md)).
* `teamster migrate up|down|status`, and `database.migrate` to verify the schema instead of
  applying it on start ([ADR 0019](docs/adr/0019-goose-migrations.md)).
* The Graph token endpoint and scope are configurable, for clouds other than the public one.

### Fixed

* Two concurrent requests for one alert no longer post two cards
  ([ADR 0021](docs/adr/0021-claim-a-card-before-posting.md)).
* Admin writes that two admins could race are guarded by the database.

## [0.3.0] — 2026-09-14

### Added

* Metrics through OpenTelemetry: a Prometheus `/metrics` listener with native histograms, and
  OTLP export ([ADR 0017](docs/adr/0017-metrics-through-opentelemetry.md)).

## [0.2.0] — 2026-09-11

### Added

* Server-rendered admin UI ([ADR 0008](docs/adr/0008-templ-tailwind-admin-ui.md)) with select
  lists, fuller record lists, and Team and channel pickers by name.
* Template preview against a sample alert, and a card editor with a palette, a starter card and
  live preview ([ADR 0014](docs/adr/0014-card-editor.md)).
* Templates with a title, text and an optional card, so the Teams activity feed shows a readable
  line ([ADR 0010](docs/adr/0010-message-shape.md)).
* Nested routes with inheritance and fan-out
  ([ADR 0011](docs/adr/0011-nested-routes.md)).
* Routing visualization at `/admin/routing`, with a check that explains which route an alert
  takes and highlights its path.
* OIDC login for the admin UI, with basic auth kept as the bootstrap path
  ([ADR 0009](docs/adr/0009-admin-authentication.md)).
* Roles `admin`, `editor` and `viewer`, enforced with Cedar, and grants that limit a role to
  Teams and channels ([ADR 0012](docs/adr/0012-role-based-authorization.md)).
* Configuration export and import as a versioned bundle, over the API and as `teamster export`
  and `teamster import` ([ADR 0013](docs/adr/0013-configuration-transfer.md)).
* A localizable UI, in English and German, chosen by `Accept-Language` or the user, with
  `ui.locale-dir` for overrides ([ADR 0015](docs/adr/0015-localizable-ui.md)).
* Liveness and readiness probes.

### Fixed

* Server connections have bounded lifetimes.
* Teams and channels sort by name with collation.

## [0.1.0] — 2026-09-09

First release: Alertmanager and universal webhooks, label-selector routing with a default route,
Adaptive Cards posted to Teams channels, alert state in SQLite so a resolve updates its card, and
an admin UI for templates, destinations and routes. Released as a signed container image and
binaries with SBOMs ([ADR 0006](docs/adr/0006-release-rebuild-sbom-signing.md)).

[Unreleased]: https://github.com/pflege-de-labs/teamster/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/pflege-de-labs/teamster/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/pflege-de-labs/teamster/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/pflege-de-labs/teamster/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/pflege-de-labs/teamster/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/pflege-de-labs/teamster/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/pflege-de-labs/teamster/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/pflege-de-labs/teamster/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/pflege-de-labs/teamster/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/pflege-de-labs/teamster/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/pflege-de-labs/teamster/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/pflege-de-labs/teamster/releases/tag/v0.1.0
