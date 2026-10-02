---
title: Configure sign-in to the admin UI
weight: 17
---

Let people sign in to **/admin** through your OpenID Connect provider, keep the local login as the
way back in, and optionally let each admin pick from their own Teams and channels.

The admin UI needs a session. Two ways to get one:

* **The local login.** `admin.username` and `admin.password` are accepted at **/admin/login**, and
  as HTTP basic auth on `/api`. Teamster refuses to start without them, and they always hold the
  `admin` role.
* **An OIDC provider**, with an authorization code flow and PKCE. Access is decided by the roles
  in a claim.

## Set the local login

```yaml
admin:
  username: admin
  password: <password>
```

As environment variables: `TEAMSTER_ADMIN_USERNAME` and `TEAMSTER_ADMIN_PASSWORD`. Keep them
configured after you add a provider: they are the way in when the provider is unreachable or the
claim is wrong. A user cannot disable the local login.

## Add an OIDC provider

{{% steps %}}

### Register a client

Register Teamster with the provider. A public client using PKCE needs no secret. Register the
redirect URI exactly: `https://<teamster-host>/admin/auth/callback`. For Keycloak, follow
[Configure Keycloak for the admin login](../keycloak/).

### Configure Teamster

```yaml
auth:
  oidc-discovery-url: "https://<host>/realms/<realm>/.well-known/openid-configuration"
  oidc-client-id: "teamster"
  oidc-client-secret: ""                 # only for a confidential client
  oidc-redirect-url: "https://teamster.example/admin/auth/callback"
  claim: "realm_access.roles"            # the claim carrying the roles
  default-role: ""                       # admin, editor, viewer, or empty for no access
```

| Key | Environment variable | Default |
| --- | --- | --- |
| `auth.oidc-discovery-url` | `TEAMSTER_AUTH_OIDC_DISCOVERY_URL` | empty: local login only |
| `auth.oidc-client-id` | `TEAMSTER_AUTH_OIDC_CLIENT_ID` | required with a discovery URL |
| `auth.oidc-client-secret` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` | empty: public client |
| `auth.oidc-redirect-url` | `TEAMSTER_AUTH_OIDC_REDIRECT_URL` | required, an absolute URL |
| `auth.oidc-scopes` | `TEAMSTER_AUTH_OIDC_SCOPES` | `profile,email,roles` |
| `auth.claim` | `TEAMSTER_AUTH_CLAIM` | `realm_access.roles` |
| `auth.groups-claim` | `TEAMSTER_AUTH_GROUPS_CLAIM` | `groups` |
| `auth.default-role` | `TEAMSTER_AUTH_DEFAULT_ROLE` | empty |
| `auth.session-ttl` | `TEAMSTER_AUTH_SESSION_TTL` | `12h` |

Teamster refuses to start when the redirect URL is not absolute: a bare path would be sent to the
provider verbatim and rejected there.

### Name the roles claim

`auth.claim` is a dotted path. For Keycloak it is usually `realm_access.roles` for a realm role,
or `resource_access.<client>.roles` for a client role.

Teamster looks for the claim in the ID token, then at the userinfo endpoint, then in the access
token. The first hit wins. Keycloak's built-in mappers put roles only in the access token, which is
enough.

### Decide what a user without a role gets

A role called `admin`, `editor` or `viewer` in the claim is that role in Teamster. A user whose
claim names none of them gets `auth.default-role`:

* `viewer` lets everyone the provider authenticates read the configuration.
* Empty lets them sign in with no access. They are shown a page telling them to ask an
  administrator for a role. Use this when everyone in the realm can reach the client.

See [Manage roles and access](../roles/) for what each role may do.

### Sign in

Open **/admin** and choose **Sign in with your identity provider**. The header names who is
signed in and their Teamster role. **User info**, in the menu under the name, lists everything the
provider sent: every role, the granted scopes, the groups and the token each was found in.

{{% /steps %}}

A role is decided at sign-in and travels with the session. A change at the provider applies the
next time that person signs in, or when the session expires after `auth.session-ttl`.

**/admin/logout** ends the Teamster session only. The provider's session survives, so signing back
in may not ask for a password.

## Let admins pick their own Teams and channels

By default the Destinations picker lists Teams and channels with Teamster's own application
permissions. When Keycloak brokers the login against Microsoft Entra, Teamster can also offer each
admin their own Teams and channels, behind a **My Teams** toggle. It uses the Entra token Keycloak
stored for that login, not a delegated registration of Teamster's own.

```yaml
auth:
  broker:
    enabled: true
    idp-alias: "microsoft"                               # the Entra link's alias in Keycloak
    token-encryption-key: "<32 random bytes, base64>"    # openssl rand -base64 32
```

| Key | Environment variable |
| --- | --- |
| `auth.broker.enabled` | `TEAMSTER_AUTH_BROKER_ENABLED` |
| `auth.broker.idp-alias` | `TEAMSTER_AUTH_BROKER_IDP_ALIAS` |
| `auth.broker.token-encryption-key` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` |

It needs `auth.oidc-discovery-url`. The key belongs to Teamster alone: it encrypts the Keycloak
tokens Teamster stores in its database, and Keycloak never sees it. Give every replica the same
key, and keep it as durable as any other secret. Losing it makes the stored tokens unreadable, and
the picker falls back to the tenant-wide list; logins and deliveries are not affected.

A local login, or an OIDC login through a realm with no Entra federation, never sees the toggle.

Keycloak needs Store Tokens, Stored Tokens Readable, the `broker` client's `read-token` role and the
delegated Graph scopes. See
[Configure Keycloak: delegated Teams and channels](../keycloak/#let-admins-pick-their-own-teams-and-channels)
and [ADR 0037](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0037-delegated-teams-via-keycloak-broker-token.md).
