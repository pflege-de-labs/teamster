# 0092. Prefer Keycloak's Identity Brokering API V2, fall back to V1

* Status: Accepted, amends [0037](0037-delegated-teams-via-keycloak-broker-token.md)
* Date: 2026-10-28

## Context

[ADR 0037](0037-delegated-teams-via-keycloak-broker-token.md) fetches each admin's stored Entra
token with `GET {issuer}/broker/{alias}/token`, bearing the admin's Keycloak access token. That
is Keycloak's Identity Brokering API V1. It is deprecated: Keycloak 26.6 added V2 as a preview,
and 26.7 made it supported, behind `--features=identity-brokering-api:v2`.

V2 differs in how a caller proves it may read the token:

* `POST` to the same URL, with `client_id`, `client_secret` and the user's access token as `token`
  in a form body. Only a confidential client may call it.
* The client, not the user, is authorised: its *Allow retrieve external tokens* capability and its
  list of allowed identity providers (client attributes `external.token.enabled` and
  `external.token.idp`). The `broker` client's `read-token` role and *Stored Tokens Readable* play
  no part.
* The access token must be issued for, or have in its audience, the client that calls.

The two versions are one versioned feature, so a Keycloak server answers exactly one of them; the
other answers `400` "Identity Brokering API feature not enabled". A deployment upgrades Teamster
and Keycloak independently, and Teamster cannot tell from discovery which version is on.

## Decision

We will try V2 first and fall back to V1 whenever Keycloak answers the V2 call with a failure.
When V2 gets no answer at all, Teamster does not try V1, which would only wait out the same
timeout again. A V2 call needs a client secret, so without `auth-oidc-client-secret` Teamster goes
straight to V1. When both fail, the error carries both responses, so a misconfigured V2 client is
not hidden behind V1's "feature not enabled".

Teamster's own OIDC client authenticates the V2 call: the access token it presents was issued to
that client, which is what V2's audience check wants.

Alternatives:

* **V2 only.** Simplest, but the picker breaks for every deployment until its Keycloak enables V2,
  and again if Teamster upgrades first.
* **A `v1|v2` setting.** Exact, but it has to be flipped in step with Keycloak, which is the
  coordination the fallback removes.
* **Fall back only on V1's "feature not enabled" answer.** Couples Teamster to Keycloak's error
  text, and an older Keycloak without the `POST` route answers differently again.

## Consequences

* Nothing changes for an existing V1 deployment beyond one extra, failing `POST` per token fetch
  when a client secret is configured. A deployment can enable V2 in Keycloak whenever it likes.
* V2 needs a confidential `teamster` client with *Allow retrieve external tokens* on and the Entra
  link's alias allowed. A public client using PKCE keeps working on V1 only.
* Once Keycloak removes V1, the fallback and the V1 setup in [Configuring
  Keycloak](../keycloak.md) can go.
