# Architecture Decision Records

Architectural decisions are recorded here, one file per decision, in
[ADR](https://adr.github.io/) format.

## Rules

* Copy [0000-template.md](0000-template.md) to `NNNN-short-title.md`, numbered sequentially.
* An accepted ADR is immutable. To change a decision, write a new ADR and set the old one to
  `Superseded by …`; only the status line of the old record may be edited.
* Any change to how components are structured, how they communicate, or which external
  dependency is used needs an ADR before the PR is merged.

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0001](0001-kong-xdg-configuration.md) | Configuration through kong with XDG file locations | Accepted |
| [0002](0002-messenger-interface.md) | The HTTP layer depends on a messenger interface | Accepted |
| [0003](0003-kong-commands-and-graceful-shutdown.md) | Commands run through kong, cancelled by signal | Accepted |
| [0004](0004-container-image.md) | Container image built multi-stage onto distroless nonroot | Accepted |
| [0005](0005-image-tagging-and-promotion.md) | Image tags come from metadata-action, releases promote | Superseded by 0006 |
| [0006](0006-release-rebuild-sbom-signing.md) | Releases rebuild, carry an SBOM and are signed | Accepted |
| [0007](0007-renovate-dependency-updates.md) | Dependency updates run through Renovate | Accepted |
| [0008](0008-templ-tailwind-admin-ui.md) | The admin UI is server-rendered with templ and Tailwind | Accepted |
| [0009](0009-admin-authentication.md) | Admin authentication by Keycloak login or local credentials | Accepted |
| [0010](0010-message-shape.md) | A template decides the title, text and card of a message | Superseded by 0028 |
| [0011](0011-nested-routes.md) | Routes form a tree and an alert can fan out | Accepted |
| [0012](0012-role-based-authorization.md) | Roles are authorized with Cedar, evaluated in-process | Accepted |
| [0013](0013-configuration-transfer.md) | Configuration moves as a versioned JSON bundle | Accepted |
| [0014](0014-card-editor.md) | Better JSON editing instead of a card designer | Accepted |
| [0015](0015-localizable-ui.md) | The UI's text lives in per-language catalogs | Accepted |
| [0016](0016-helm-chart.md) | The Helm chart ships in this repository and deploys a single SQLite writer | Superseded by 0023 |
| [0017](0017-metrics-through-opentelemetry.md) | Metrics are recorded once against OpenTelemetry and exported as native histograms | Accepted |
| [0018](0018-store-takes-a-context.md) | Every store call takes a context | Accepted |
| [0019](0019-goose-migrations.md) | The schema is a numbered set of goose migrations | Accepted |
| [0020](0020-sqlc-generated-queries.md) | Queries are generated from SQL by sqlc | Accepted |
| [0021](0021-claim-a-card-before-posting.md) | A card is claimed before it is posted | Accepted |
| [0022](0022-postgres-second-backend.md) | Postgres is the second storage backend | Accepted |
| [0023](0023-chart-deploys-either-shape.md) | The chart deploys either shape | Accepted |
| [0024](0024-trivy-image-scanning.md) | Scan images with Trivy and report to SecObserve | Accepted |
| [0025](0025-shell-completion.md) | Shell completion is generated from the command tree | Accepted |
| [0026](0026-alerts-in-a-persons-chat.md) | Alerts reach a person's chat through a Bot Framework bot | Accepted, amended by [0045](0045-channel-delivery-through-the-bot.md), [0047](0047-a-route-targets-a-channel-or-yourself.md), [0065](0065-your-own-chat-is-found-from-your-sign-in.md) |
| [0027](0027-notify-on-link-displacement.md) | A displaced recipient conversation is notified, not left silent | Accepted |
| [0028](0028-self-service-unlink-and-code-cancellation.md) | Self-service unlink resolves from the session, and a code can be cancelled | Accepted, amended by [0061](0061-no-opt-out-when-installed-for-everyone.md) |
| [0029](0029-templates-are-markdown.md) | Message text is authored as Markdown and sanitized once for both transports | Accepted |
| [0030](0030-teams-v2-compatible-webhooks.md) | Accept the payloads a Teams V2 webhook accepts | Accepted, amended by [0040](0040-teams-v2-endpoint-templates.md) |
| [0031](0031-vendored-browser-libraries-pinned-and-verified.md) | Vendored browser libraries are pinned in a manifest and verified in CI | Accepted |
| [0032](0032-retiring-a-link-from-the-chat.md) | A link can be retired from the chat it belongs to | Accepted, amended by [0061](0061-no-opt-out-when-installed-for-everyone.md) |
| [0033](0033-split-httproute-external-and-internal.md) | Split the chart's HTTPRoute into external and internal | Accepted |
| [0034](0034-fan-out-across-independent-routes.md) | Every matching root route delivers, not only the highest priority one | Accepted |
| [0035](0035-a-message-without-a-status-is-delivered-once.md) | A message posted without a status is delivered once, not tracked | Superseded in part by [0056](0056-events-not-alerts.md), refined by [0063](0063-a-message-names-its-recipients.md) |
| [0036](0036-direct-content-when-a-route-has-no-template.md) | A route with no template sends the payload's own title, text and card | Superseded in part by [0039](0039-built-in-default-message.md) |
| [0037](0037-delegated-teams-via-keycloak-broker-token.md) | Delegated Teams/Channels via Keycloak broker token pass-through | Accepted |
| [0038](0038-global-default-destination.md) | Catch every unclaimed message in a global default destination | Accepted |
| [0039](0039-built-in-default-message.md) | Send a built-in default message when nothing says what a message looks like | Accepted |
| [0040](0040-teams-v2-endpoint-templates.md) | A Teams V2 endpoint may name a template | Accepted |
| [0041](0041-editor-completion-from-sampled-labels.md) | Complete templates and routes from labels sampled off incoming alerts | Accepted |
| [0042](0042-sidebar-navigation-and-user-menu.md) | Move page links to a sidebar and account controls to a user menu | Accepted, partly superseded by 0054 |
| [0043](0043-session-keeps-sign-in-identity.md) | Keep what the identity provider said on the session row | Accepted |
| [0044](0044-webhook-access-tokens.md) | Authenticate the alert webhooks with issued Bearer tokens | Accepted |
| [0045](0045-channel-delivery-through-the-bot.md) | Deliver channel messages through the Bot Framework bot | Accepted, amended by [0049](0049-a-channel-post-is-one-teams-message.md), [0059](0059-install-the-teams-app-for-every-member.md); superseded in part by [0069](0069-the-bot-answers-commands-in-team-channels.md) |
| [0046](0046-structured-logging-and-error-boundary.md) | Log through slog and answer errors at one boundary | Accepted |
| [0047](0047-a-route-targets-a-channel-or-yourself.md) | A route targets a channel or a person, and a person only themselves | Accepted, amended by [0051](0051-root-routes-need-a-target-refinements-a-template.md), [0062](0062-a-route-may-deliver-to-the-people-a-message-names.md) |
| [0048](0048-bot-answers-commands-in-the-personal-chat.md) | The bot answers commands in the personal chat | Accepted, amended by [0068](0068-commands-are-words-addressed-to-the-bot.md) |
| [0049](0049-a-channel-post-is-one-teams-message.md) | A channel post is one Teams message | Accepted, amended by [0057](0057-channel-posts-carry-a-feed-summary.md) |
| [0050](0050-catch-all-template-is-a-setting.md) | The catch-all route's template is a setting | Accepted |
| [0051](0051-root-routes-need-a-target-refinements-a-template.md) | A root route needs a target, and a refinement that keeps it needs a new template | Accepted |
| [0052](0052-the-receiving-webhook-is-a-label.md) | The receiving webhook is a label | Accepted |
| [0053](0053-templates-name-the-webhooks-they-handle.md) | Templates name the webhooks they handle | Accepted |
| [0054](0054-apply-sidebar-state-before-first-paint.md) | Apply the sidebar state before first paint | Accepted |
| [0055](0055-each-webhook-has-a-default-template.md) | Each webhook has a default template | Accepted |
| [0056](0056-events-not-alerts.md) | Model what arrives as an event, not an alert | Accepted |
| [0057](0057-channel-posts-carry-a-feed-summary.md) | Send the title as the activity summary | Accepted |
| [0058](0058-preview-shows-the-wire-payload.md) | The preview shows the payload delivery sends | Accepted |
| [0059](0059-install-the-teams-app-for-every-member.md) | Install the Teams app for every member of the tenant | Accepted |
| [0060](0060-directory-users-are-not-recipients.md) | Directory users are kept apart from linked recipients | Accepted |
| [0061](0061-no-opt-out-when-installed-for-everyone.md) | Nobody opts out while the app is installed for everyone | Accepted |
| [0062](0062-a-route-may-deliver-to-the-people-a-message-names.md) | A route may deliver to the people a message names | Accepted |
| [0063](0063-a-message-names-its-recipients.md) | A message names its recipients, and the answer says who was not reached | Accepted |
| [0064](0064-chat-claims-for-people-share-the-recipient-table.md) | Chat claims for addressed people share the recipient claim table | Accepted |
| [0065](0065-your-own-chat-is-found-from-your-sign-in.md) | Your own chat is found from your sign-in when the app is installed for everyone | Accepted |
| [0066](0066-keep-separate-graph-and-bot-registrations.md) | Keep the Graph and bot registrations separate | Accepted |
| [0067](0067-times-in-the-admin-ui-follow-a-chosen-zone.md) | Show times in the admin UI in UTC or the browser's zone | Accepted |
| [0068](0068-commands-are-words-addressed-to-the-bot.md) | Name the bot's commands without a slash | Accepted |
| [0069](0069-the-bot-answers-commands-in-team-channels.md) | The bot answers commands in team channels | Accepted |
| [0070](0070-audit-configuration-changes-at-the-store.md) | Audit configuration changes at the store boundary | Accepted |
| [0071](0071-publish-audit-events-to-nats-jetstream.md) | Publish audit events to NATS JetStream | Accepted |
| [0072](0072-remember-who-signed-in.md) | Remember who signed in, and let an admin disable them | Accepted |
| [0073](0073-authorize-from-a-versioned-policy-snapshot.md) | Authorize from a versioned policy snapshot | Accepted |
