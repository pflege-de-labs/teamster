# 0044. Authenticate the alert webhooks with issued Bearer tokens

* Status: Accepted; issuing and scope superseded by [0077](0077-scoped-tokens-answer-to-their-creator.md)
* Date: 2026-09-28

## Context

`/webhook/alertmanager` and `/webhook/universal` accepted one secret, `webhook.token`, and only in
a custom `X-Teamster-Token` header. That caused three problems in practice:

* Alertmanager's standard way to authenticate a webhook is `http_config.authorization` (a Bearer
  token), and the Prometheus Operator's `AlertmanagerConfig` offers nothing else. Teamster ignored
  that header, so a correctly configured sender got a bare 401.
* One secret shared by every sender cannot be revoked for one of them. Rotating it means changing
  the config, restarting, and updating every sender at the same moment.
* The token could only come from configuration, so nobody could issue one from the admin UI. ADR
  0009 had already named per-integration tokens as the later fix.

The Teams V2 endpoints (ADR 0030) already store a SHA-256 digest of a random token per endpoint,
so the storage pattern exists.

## Decision

We will accept `Authorization: Bearer <token>` on both alert webhooks. A token is valid when it
matches `webhook.token` or an **access token** issued at `/admin/tokens` or `POST /api/tokens`.

* An access token has a name, a creator, a creation time and a last-used time, and is stored as
  a SHA-256 digest in `access_tokens`. It is shown once. It is 256 random bits behind a `tst_`
  prefix, so secret scanners can recognise one.
* A token is unscoped: it admits a sender to both webhooks and every route. Scoping a token to
  an endpoint or a role's grants can come later, as a column the old release ignores.
* Only admins can issue, list or revoke tokens (`administer` on `AccessToken`), because a token
  reaches every route.
* The request looks the token up by its digest. Both values are digests of 256-bit secrets, so
  comparing them in SQL leaks nothing a timing attack could use.
* `last_used_at` is written at most once an hour per token, so an alert storm does not become a
  storm of database writes.
* A store failure during the lookup answers 503, not 401, so an outage does not look like a
  wrong secret.
* `webhook.token` becomes optional, and `X-Teamster-Token` is still accepted for any token. Both
  are deprecated, not removed. Removing them now would break every existing sender. It would
  also leave a GitOps-managed Alertmanager with no way to get a token before somebody signs in.

Alternatives we rejected:

* **Bearer on the shared token alone.** This fixes the 401 but not revocation.
* **Basic auth.** It gives nothing over Bearer, and it collides with the admin API's credentials
  in the same header.
* **Tokens in configuration as a list.** Every new token still needs a restart, and a config
  file is a worse place for secrets than a table of digests.

## Consequences

* Alertmanager and `AlertmanagerConfig` work with their standard `authorization` block.
* Each sender can have its own token and lose it without disturbing the others. The token list
  shows which senders are active.
* Tokens live in the database, so `export`/`import` does not carry them: a digest cannot be
  handed to a new sender. A restored installation has to issue new tokens.
* The migration only adds a table, so the previous release runs against it unchanged.
* A later breaking release can drop `X-Teamster-Token` and possibly `webhook.token`. The roadmap
  tracks this.
