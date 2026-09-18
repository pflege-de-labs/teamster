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
| [0026](0026-alerts-in-a-persons-chat.md) | Alerts reach a person's chat through a Bot Framework bot | Accepted |
| [0027](0027-notify-on-link-displacement.md) | A displaced recipient conversation is notified, not left silent | Accepted |
| [0028](0028-self-service-unlink-and-code-cancellation.md) | Self-service unlink resolves from the session, and a code can be cancelled | Accepted |
| [0029](0029-templates-are-markdown.md) | Message text is authored as Markdown and sanitized once for both transports | Accepted |
| [0030](0030-teams-v2-compatible-webhooks.md) | Accept the payloads a Teams V2 webhook accepts | Accepted |
| [0031](0031-vendored-browser-libraries-pinned-and-verified.md) | Vendored browser libraries are pinned in a manifest and verified in CI | Accepted |
| [0032](0032-retiring-a-link-from-the-chat.md) | A link can be retired from the chat it belongs to | Accepted |
