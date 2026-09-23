# 0037. Delegated Teams/Channels via Keycloak broker token pass-through

* Status: Accepted, amends [0009](0009-admin-authentication.md)
* Date: 2026-09-23

## Context

The Destinations admin UI lists Microsoft Teams and channels through `internal/graph`'s Graph
client, which authenticates with its own app-only, client-credentials Entra registration
(`graph-*`). That list is tenant-wide: whatever the app registration was granted, every admin sees,
regardless of which Teams they themselves belong to. For a large tenant that is every Team in the
organisation in one dropdown, most of which the signed-in admin has no business posting to and did
not ask to see.

Separately, [ADR 0009](0009-admin-authentication.md) added Keycloak as an OIDC login for the admin
UI, unified with local credentials into DB-backed sessions (`models.Session`). Where Keycloak
brokers that login against Microsoft Entra as an upstream identity provider — the common shape for
an organisation that already federates through Keycloak — Keycloak's own admin console offers
**Store Tokens** on the identity provider link: once enabled, Keycloak retains the upstream Entra
token from each federated login, and `GET {issuer}/broker/{alias}/token`, given the admin's own
still-valid Keycloak access token as a bearer credential, returns that stored Entra token —
refreshing it upstream itself if it has gone stale. Teamster never has to talk to Entra's token
endpoint for this at all.

That is the one existing mechanism that can turn "list every Team in the tenant" into "list the
Teams I am actually in," without minting Teamster a delegated Entra registration of its own, asking
every admin for delegated consent to a second app, or reimplementing Entra token refresh logic
Keycloak already does. The alternative — a delegated Entra app registration with its own
authorization-code-with-PKCE consent flow inside the admin UI — was considered and rejected: it
would duplicate the login Keycloak already performs, in a second identity system, for a strictly
narrower convenience (which Teams to show in one dropdown).

## Decision

We will let Teamster ask Keycloak's broker endpoint for the Entra token behind a signed-in admin's
own OIDC login, and use it to call Microsoft Graph on that admin's behalf for exactly one thing:
listing their own Teams and channels in the Destinations picker. Everything else about a
destination — grants, routes, delivery — is unchanged and still goes through the app-only Graph
client.

**This means Teamster custodies a live Keycloak bearer credential per session**, not merely an
opaque session id. That is a materially larger blast radius than `sessions` held before: a stolen
row is now a stolen path to Entra, mediated by Keycloak, for as long as the credential remains
valid and refreshable. We are accepting that tradeoff explicitly, for the size of the convenience,
rather than choosing a safer but more elaborate alternative (a delegated Entra app of its own, or
not offering a per-admin list at all). The mitigations below are what make that acceptable:

* **Encrypted at rest.** A new `broker_tokens` table holds only ciphertext:
  `internal/cryptutil` seals the access and refresh token with AES-256-GCM, keyed by a
  32-byte key from `auth-broker-token-encryption-key`, with the session id as additional
  authenticated data — a row cannot be decrypted as if it belonged to a different session, so a
  copy-paste across rows fails rather than silently succeeding.
* **Scoped to the feature, off by default.** `auth-broker-enabled` gates the whole thing; a
  deployment that does not opt in stores nothing and exposes no new route with meaningful
  behaviour behind it — see [Local-credential sessions](#not-decided-here) below for what an
  unconfigured or local-only deployment sees instead.
* **Bounded lifetime, not a bearer of everything.** The stored credential is Keycloak's own, not an
  Entra one Teamster minted — it can do nothing at Entra directly; it can only ask Keycloak's
  broker endpoint, which enforces whatever consent and token lifetime Keycloak itself was
  configured with. A revoked Keycloak session or a logged-out user makes the stored refresh token
  as dead as the session it came from.
* **Fails fast on a dead credential.** A hard refresh failure — Keycloak firmly rejecting the
  refresh token, an `oauth2.RetrieveError` with a 4xx status — deletes the row immediately, so a
  revoked credential is retried once, not forever. A transient failure to reach Keycloak leaves it
  in place and is reported as a normal `502`, exactly like an existing Graph failure.
* **Cascade-deleted with the session.** `DeleteSession` is overridden on both backends to remove
  the matching `broker_tokens` row in the same transaction — this repo declares no SQL foreign
  keys (see the migration's own comment for why), so the cascade is Go-level, the same pattern
  `deleteRecipientCascade` already established.

**Mechanism**, concretely:

* `internal/httpserver/oidc.go`'s `handleAuthCallback` persists the Keycloak token it already
  holds — the one it exchanges the authorization code for — into `broker_tokens`, keyed by the
  session id `startSession` just created. `startSession` now returns that id rather than only an
  error, which is the one signature change this requires; `handleLocalLogin` ignores it, having no
  broker token to store.
* `internal/httpserver/broker.go`'s `brokerTokenSource` loads and decrypts a session's stored
  token, and wraps it in an `oauth2.TokenSource` built from the same `oauth2.Config` (`provider.
  Endpoint()`, discovered the same way login already discovers it) — refreshing against Keycloak's
  own token endpoint is ordinary OAuth2, not anything broker-specific, and re-persists a refreshed
  token, re-encrypted, so a second request in the same window does not refresh again for nothing.
* `internal/graph.BrokerClient` is the Graph half, living in `internal/graph` rather than
  `internal/httpserver` so it can share `Team`, `Channel` and the package's own `instrumentation`
  interface instead of duplicating either — the alternative, a second package, bought nothing but a
  cross-package type conversion at every call site. It is a plain `*http.Client` with a bearer
  header set per request, deliberately not `Client`'s own `clientcredentials`-wrapped one: the two
  credential shapes do not mix, and reusing the wrong one would authenticate the request with
  Teamster's own app-only token instead of the admin's delegated one.
* `GET /api/graph/my-teams` and `GET /api/graph/my-teams/{id}/channels` mirror the existing
  tenant-wide pair, gated on `auth-broker-enabled` and the session's `Source` being `oidc`,
  answering `409` otherwise. There is no `directoryCache` for either: the result is per admin, and
  caching it the same way would leak one admin's Teams into another's picker.
* The Destinations picker (`pickers.js`) offers an "All Teams"/"My Teams" toggle only when the
  server says both a session and the feature allow it (`views.Page.BrokerAvailable`), switching
  which pair of endpoints it calls; typing an id by hand remains the fallback either way.

### Not decided here

A local login has no Keycloak token to broker and always sees the tenant-wide list, or the toggle
absent entirely — this is existing behaviour extended, not a new restriction, since local logins
never had a per-admin list to offer. Whether to extend the same delegated-token idea to any other
Graph-backed convenience is left for if one is ever asked for; nothing here generalises beyond
Teams/Channels listing.

## Consequences

* Amends [ADR 0009](0009-admin-authentication.md)'s consequence "`internal/graph` and the Entra
  registration are untouched. Alert delivery does not depend on anyone being logged in." That
  remains true for delivery and for the app-only credential itself — neither changes — but a
  signed-in admin's own login now, optionally, reaches into Entra for a second purpose. The
  qualification is narrow: only `auth-broker-enabled` deployments, only the Destinations picker,
  never delivery.
* A schema addition only: `broker_tokens` is a new table, and `sessions` is untouched. The previous
  release still opens a database with this migration applied; it simply never reads the new table.
* Operators enabling the feature must additionally configure the Entra app registration for
  delegated `Team.ReadBasic.All`/`Channel.ReadBasic.All` consent, and Keycloak's Entra identity
  provider link for Store Tokens and Stored Tokens Readable — see
  [Configuring Keycloak](../keycloak.md).
* `auth-broker-token-encryption-key` is a new secret to manage, on top of the OIDC client secret
  and the Graph/Bot credentials already required. Losing it makes every stored broker token
  permanently unopenable — the same failure mode as any other lost encryption key — which is an
  outage for the delegated picker, not for delivery or for login itself.
