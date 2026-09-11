# 0009. Admin authentication by Keycloak login or local credentials

* Status: Accepted, authorization part superseded by [0012](0012-role-based-authorization.md)
* Date: 2026-09-10

## Context

The admin UI is protected by HTTP basic auth against a single username and password from the
configuration. Every change is therefore attributable to `admin` and nobody else, the credential is
shared by everyone who administers the service, and rotating it means editing config and
restarting. A browser also caches it for the session with no way to sign out.

Milestone 2 of [the roadmap](../roadmap.md) replaces that with an identity provider. The provider
here is Keycloak, reached through standard OIDC discovery.

**This has nothing to do with the Microsoft Entra credentials.** Those are a machine credential
used by `internal/graph` to post Adaptive Cards through the Graph API, configured under `graph-*`.
The login described here is a separate, human-facing identity system configured under `auth-*`.
Two unrelated OIDC-shaped things live in one binary, and confusing them would hand an operator's
browser a credential meant for posting messages. The configuration prefixes exist to keep them
apart.

## Decision

Two ways in, both ending in the same session:

* **Keycloak**, via authorization code flow with PKCE. The provider is configured by the URL of
  its `/.well-known/openid-configuration`, not by an issuer from which that location is guessed,
  because a provider is free to publish the document anywhere. The issuer that ID tokens are
  verified against is read from that document.

  The registered client is **public**: `teamster`, PKCE `S256`, no client secret. The code
  exchange is therefore authenticated by the PKCE verifier alone rather than by a client
  credential. That is the point of PKCE and it removes a secret from the configuration, but it
  means the redirect URI registered at the provider is the only thing binding the flow to this
  service — so it must be registered exactly, not as a wildcard. The client secret setting stays
  in the configuration as an optional value, because a confidential client is equally valid.
* **Local username and password**, entered in a login form rather than a browser basic-auth
  dialog. This is the bootstrap path for a deployment with no IdP, and the way back in when
  Keycloak is unreachable. It authenticates against the existing configured credentials.

### Sessions

A successful login of either kind creates a row in a new `sessions` table in SQLite and sets an
opaque session id in a cookie marked `HttpOnly`, `SameSite=Lax`, and `Secure` when the request
arrived over TLS.

State in the database rather than a signed cookie, because a session must be revocable: logging
out has to end the session everywhere, not merely drop one browser's copy. Expired rows are swept
periodically and on access.

### Authorization

Authentication is not authorization. A configured claim decides who may administer the service:

* `auth-claim` names the claim, as a dotted path, because Keycloak nests roles under
  `realm_access.roles` and `resource_access.<client>.roles` rather than exposing a flat claim.
  A `groups` claim requires a mapper on the client scope in Keycloak; realm roles are present by
  default.
* `auth-allowed` lists the accepted values. A token carrying none of them is authenticated but
  refused, and the refusal says so rather than looping back to the IdP.
* The claim is looked for in the ID token, then at the userinfo endpoint, then in the access token,
  and the first hit wins. Keycloak's built-in role mappers populate the access token and leave the
  ID token without roles, so reading only the ID token refuses a correctly configured realm. The
  access token payload is read without verifying its signature: it arrives from the token endpoint
  over TLS beside an ID token that was verified, and is only read to look up membership.
* Configuring an issuer without any accepted value refuses to start. Failing closed is the only
  safe reading: the alternative is a deployment that quietly admits everyone the realm will
  authenticate.

The realm at `https://login.fsb.p4e.io/auth/realms/internal` publishes both a `groups` and a
`roles` scope, so either a `groups` claim or `realm_access.roles` can carry membership. Which one
is used is configuration, not code.

A user who logs in with the local credentials is authorized implicitly; possession of the
configured password is the check.

### What stays on basic auth

`/api` keeps accepting HTTP basic auth. It is the scriptable interface, `curl -u` against it is
documented in the README, and automation cannot complete an authorization code flow. Browsers
never see basic auth again: `/admin` accepts only a session.

This is the one deliberately conservative choice here, taken to avoid breaking existing
integrations. Replacing it with per-integration API tokens is a reasonable later ADR.

### Routes

| Path | Purpose |
| --- | --- |
| `GET /admin/login` | Login form, plus a "Sign in with Keycloak" link when an issuer is configured |
| `POST /admin/login` | Local credential login |
| `GET /admin/auth/start` | Begins the OIDC flow |
| `GET /admin/auth/callback` | Registered redirect URI; exchanges the code and creates the session |
| `POST /admin/logout` | Deletes the session and clears the cookie. It does not yet call the provider's `end_session_endpoint`: that needs a `post_logout_redirect_uri` registered at the provider, so signing out of Teamster leaves the Keycloak session intact for now |

`state`, `nonce` and the PKCE verifier are held server-side in the same table as short-lived
pre-authentication rows, so a forged callback cannot complete a flow this service did not start.

`POST /admin/login` and `POST /admin/logout` go through the existing `formPost` guard, which
already refuses a request that cannot prove its origin.

## Consequences

* Changes become attributable to a person, and access is granted and revoked in Keycloak rather
  than by redeploying a password.
* The service gains session state. A restart no longer signs everyone out, but the database now
  holds security-relevant rows that must be swept.
* TLS matters more than before. A session cookie is a bearer token for the admin UI, so the
  existing requirement to terminate TLS in front of Teamster becomes a hard one.
* Misconfiguring the claim path locks everyone out of a deployment with no local credentials
  configured. The local login exists so that is recoverable, and a refusal names the claim, where
  it looked and the values it found, so the fix does not require reading the provider's logs.
* [Configuring Keycloak](../keycloak.md) documents the client, the role and the mapper, including
  that a client role lives at `resource_access.<client>.roles` and never under `realm_access.roles`.
* `internal/graph` and the Entra registration are untouched. Alert delivery does not depend on
  anyone being logged in.
