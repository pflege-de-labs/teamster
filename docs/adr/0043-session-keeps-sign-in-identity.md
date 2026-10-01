# 0043. Keep what the identity provider said on the session row

* Status: Accepted; "groups decide nothing" superseded by [0074](0074-local-groups-and-provider-groups.md)
* Date: 2026-09-28

## Context

A user who signs in and finds a control missing, or lands on the no-access page, has no way to see
what the identity provider actually sent. The session stored only the roles derived from the claim.
The scopes granted, the groups, the raw claim values and which token they were found in were all
discarded at the callback. Debugging a Keycloak mapper meant decoding tokens by hand.

## Decision

We will store a curated identity on the session and show it on `/admin/userinfo`.

* `sessions.identity` is a new `TEXT NOT NULL DEFAULT ''` column holding JSON (`models.Identity`).
  It records the issuer, email, granted scopes, groups, the role claim values and the token each
  lookup was found in. A local login stores nothing.
* Granted scopes come from the token response's `scope` field. When the provider leaves it out, they
  are the scopes requested, per RFC 6749 §5.1.
* Groups come from the new `auth.groups-claim` setting (default `groups`). They are looked up in the
  same id token → userinfo → access token order as roles, and userinfo is fetched once for both.
  Groups decide nothing.
* No token and no complete claim set is stored, only the fields the page shows.
* `/admin/userinfo` requires a session but sits outside `authorize`. A user with no role is who most
  needs to read it.

Alternatives considered:

* Storing the raw ID token or all claims, which keeps personal data nobody asked for and invites
  using it for decisions.
* A separate table, which adds a join for a 1:1 relation that dies with the session.
* Decoding tokens on each page view, which would require keeping the tokens.

## Consequences

The migration is additive. The previous release inserts sessions without the column and reads a
fixed column list, so it runs against the new schema. Sessions created before the upgrade have an
empty identity, and the page asks the user to sign in again. The identity is fixed at sign-in, as the
roles already are, so a change at the provider shows at the next sign-in.
