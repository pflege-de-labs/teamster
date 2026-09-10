# 0009. Admin authentication by Keycloak login or local credentials

* Status: Proposed
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

* **Keycloak**, via authorization code flow with PKCE. Endpoints come from discovery at the
  configured issuer, so no endpoint is hardcoded and any conformant provider works.
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
| `POST /admin/logout` | Deletes the session, then redirects to Keycloak's `end_session_endpoint` when one is advertised |

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
  configured. The local login exists so that is recoverable.
* `internal/graph` and the Entra registration are untouched. Alert delivery does not depend on
  anyone being logged in.
