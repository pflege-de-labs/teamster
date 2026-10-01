# 0077. Scoped tokens answer to their creator, on every use

* Status: Accepted
* Date: 2026-10-01

## Context

Only admins could issue webhook access tokens, and a token admitted its sender to both webhooks
and every route ([ADR 0044](0044-webhook-access-tokens.md)). Teams want to mint tokens for their
own senders. A token minted by a user must not reach further than that user may. It must also stop
working when the user's access stops: when they are disabled
([ADR 0072](0072-remember-who-signed-in.md)), when they leave a group, or when their webhook level
is taken away ([ADR 0076](0076-webhook-permissions-and-access-overviews.md)).

The rollback rule applies with force here. The previous release matches a presented token against
`token_hash`. If it found a scoped token there, it would admit it everywhere.

## Decision

* **A token has a scope.** New tokens carry `scope`, the webhooks they may send to. Minting requires
  a scope, and every webhook in it must be one the minter may `use` at the time.
* **Self-service.** Anyone who may use a webhook mints at `/admin/tokens`. They see and revoke their
  own tokens. Admins and webhook admins (`administer` on `Webhook::"*"`) see and revoke everyone's.
  Tokens are record endpoints ([ADR 0075](0075-own-and-share-records-through-generated-policies.md)),
  so a grant holder without a role reaches the handlers, and the handlers decide.
* **The scope is a generated policy.** It renders as
  `@id("token:<id>") permit (principal == Token::"<id>", action == Action::"use", resource == Webhook::"…");`
  and loads into the snapshot like a grant. Creating or deleting a token bumps the generation.
* **Two checks on every use.** `Authorizer.AllowToken` asks Cedar twice:
  1. whether the token may use the webhook;
  2. whether its creator may.

  The creator is rebuilt from the user registry, with the roles and provider groups of their last
  sign-in and their current local groups and grants. A disabled or unknown creator is refused. The
  configured local admin has no registry row until they sign in, and answers as admin. A refusal is
  `403`, logged with the token and creator and counted as `forbidden`. An unreadable snapshot or
  registry is `503`.
* **Rollback safety.** A scoped token's digest is stored in a new column, `scoped_token_hash`. Its
  `token_hash` holds `scoped:<id>`, which no SHA-256 digest in hex equals. The previous release
  therefore refuses a scoped token instead of admitting it unscoped.
* **Old tokens.** Tokens from before have an empty scope, and they and `webhook.token` keep today's
  behaviour: both webhooks, whoever made them. Re-minting binds a sender.

Alternatives considered:

* **Copying the creator's permissions into the token at minting.** A frozen copy outlives the
  permission it copied, which is what the requirement rules out.
* **The scope check alone, in Go.** It is simpler, but the scope would not be in Cedar or in the
  overview, and the two checks would be decided in two languages.
* **Narrowing the existing `token_hash`** (moving every digest to a new column). That breaks the
  previous release for every token, not only the new ones.

## Consequences

* A scoped webhook request costs a generation read and a user registry lookup, on top of the token
  lookup.
* Roles and provider groups reach a token at its creator's next sign-in, as they reach the
  creator's session. Local groups and grants reach it at once.
* Token names stay unique across the installation, so two users cannot both call a token
  `staging`.
* `access_tokens` gains `scope` (`NOT NULL DEFAULT ''`) and `scoped_token_hash` (nullable, unique):
  SQLite `0028`, Postgres `0025`.
* ADR 0044's "every token reaches every route, whoever made it" now holds only for unscoped
  tokens.
