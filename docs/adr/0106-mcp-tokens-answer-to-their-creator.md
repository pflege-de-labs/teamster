# 0106. MCP tokens answer to their creator, in a table of their own

* Status: Accepted
* Date: 2026-11-11

## Context

AI assistants need a credential to call Teamster's MCP endpoint. It must reach no further than the
person who made it, and stop working when their access stops, as scoped webhook tokens do
([ADR 0077](0077-scoped-tokens-answer-to-their-creator.md)).

A row in `access_tokens` would not do. The previous release, and this one, match a presented
webhook token against `access_tokens` alone, so an MCP token stored there would be admitted to both
webhooks: a token with an empty `scope` is a legacy token that opens both, and both releases look
tokens up there. Every signed-in user must also be able to mint one, including a viewer, who has no
webhook to use.

## Decision

* **A table of its own.** `mcp_tokens` holds `token_hash`, `created_by`, `access` (`read` or
  `write`) and `last_used_at`. Nothing that reads `access_tokens` can see it, and the other way
  round. The token is `tmcp_` and 256 random bits, so a secret scanner recognises it and nobody
  mistakes it for a webhook token. Only the SHA-256 digest is stored.
* **The creator is rebuilt on every use.** `mcpAuth` looks the token up by digest and asks
  `tokenCreator` for the creator, with the roles and provider groups of their last sign-in, as the
  webhooks do. It then serves the request as that user. A disabled or unknown creator is refused
  with `403`; an unreadable registry is `503`. `access` only narrows: a `read` token cannot write
  whatever its creator may do, and neither can any token while `mcp.read-only` is on.
* **Any signed-in user mints.** The token carries no permission of its own, so there is nothing to
  check at minting. Users see and revoke their own tokens; webhook admins and admins see everyone's.
* **Names are unique per creator**, `(created_by, name)`. A global name would refuse a second
  user's `laptop` and tell them someone else already has one.
* **`authorize` has an exact-path exception.** While `mcp.enabled` is on, `/admin/tokens`,
  `/admin/tokens/mcp/new` and `/admin/tokens/mcp/delete` reach their handlers for any signed-in
  principal, and the handlers decide whose tokens are shown and revoked. Without it, a viewer
  could not mint a read token. The paths are matched exactly, so no other `/admin/tokens` route
  opens.
* **Minting and revoking are audited** as `mcp_token.create` and `mcp_token.delete`, without the
  digest.
* **Off unless asked for.** `mcp.enabled` and `mcp.read-only` both default to false.

Alternatives considered:

* **A column on `access_tokens`.** Narrowing the webhook match to exclude it is not possible for
  the previous release, which would admit the token to the webhooks.
* **Copying the creator's permissions into the token.** Refused in ADR 0077: a frozen copy outlives
  the permission it copied.
* **Installation-wide names.** Simpler, but it leaks other users' token names.

## Consequences

* A new table is additive, so the previous release ignores it: SQLite `0034`, Postgres `0031`.
* With `mcp.enabled` on, a viewer reaches `/admin/tokens` and can mint and revoke their own MCP
  tokens there. The webhook section stays as it was for them, and its forms still check the caller.
* This change adds the tokens only. The `/mcp` endpoint that accepts them follows.
