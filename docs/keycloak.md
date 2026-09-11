# Configuring Keycloak for the admin login

Teamster signs administrators in with an authorization code flow and PKCE, then grants access on
the strength of a claim. This is the identity provider for **people**; the Microsoft Entra
credentials under `graph-*` are a machine credential for posting cards and are unrelated.

## The client

Create a client in the realm that holds your operators:

| Setting | Value |
| --- | --- |
| Client ID | `teamster`, matching `auth.oidc-client-id` |
| Client authentication | **Off** — a public client, so no secret to store |
| Authentication flow | Standard flow only |
| PKCE method | `S256` |
| Valid redirect URI | the exact absolute URL, e.g. `https://teamster.example/admin/auth/callback` |

A public client is the recommended setup: PKCE binds the code exchange to the browser that started
it, so there is no secret in the configuration. A confidential client works too — set
`auth.oidc-client-secret`.

Register the redirect URI exactly. With no client secret it is the only thing binding the flow to
this service, so a wildcard weakens the login. Teamster refuses to start if
`auth.oidc-redirect-url` is not an absolute URL, because a bare path is sent to Keycloak verbatim
and rejected there, far from the configuration that caused it.

Point `auth.oidc-discovery-url` at the realm's document:

```text
https://<host>/realms/<realm>/.well-known/openid-configuration
```

Older Keycloak deployments serve it under `/auth/realms/<realm>/...`. Teamster fetches whatever URL
you configure rather than deriving it, so either works.

## Where the roles live

Teamster reads roles from a claim. Two shapes, and they live in different places:

| Role kind | Claim path |
| --- | --- |
| Realm role | `realm_access.roles` |
| Client role on the `teamster` client | `resource_access.teamster.roles` |

A **client** role does not appear under `realm_access.roles`. Configuring the wrong path is the
most common way to lock everyone out, and because Teamster fails closed, the local login is then
the only way back in.

Assign the role to the operators who should administer the service, and set:

```yaml
auth:
  claim: "realm_access.roles"      # or resource_access.teamster.roles
```

## Roles beyond signing in

Name the roles in Keycloak exactly `admin`, `editor` and `viewer` — as realm roles, or as client
roles on the `teamster` client — and assign them. Those three are the minimum: they are what
Teamster's shipped policies define. Roles beyond them are read too and reach the policies by name,
so defining `auditor` here means writing a policy for `Role::"auditor"` in
`internal/authz/policies.cedar`; until then it grants nothing.

Teamster reads them by name:

```yaml
auth:
  claim: "realm_access.roles"      # or resource_access.teamster.roles
  default-role: "viewer"           # or leave empty for no access
```

A user carrying several holds all of them, and the policies decide. A user carrying none of the
three gets `default-role`; if that is empty they sign in with no access and are told to ask an
administrator for a role — the setting to use when everyone in the realm can reach the client.

Keycloak's own realm roles (`offline_access`, `default-roles-<realm>`) arrive as roles as well. No
policy mentions them, so they grant nothing, and they do not stand in for a Teamster role when the
default is applied.

## Where Teamster looks for the claim

In order, first hit wins:

1. the ID token
2. the userinfo endpoint
3. the access token

**No mapper changes are required.** Keycloak's built-in role mappers populate the access token and
leave the ID token without roles, which the third step covers.

### Putting the roles in the ID token instead

If you would rather the roles travel in the ID token, add a dedicated mapper to the client:

1. **Clients → `teamster` → Client scopes → `teamster-dedicated` → Add mapper → By configuration**
2. Choose **User Realm Role**, or **User Client Role** and set *Client ID* to `teamster`
3. Set *Token Claim Name* to `realm_access.roles` or `resource_access.teamster.roles` to match
   `auth.claim`. Keycloak reads `.` as nesting, so this produces the same shape as the built-in
   mappers
4. Enable *Multivalued* and *Add to ID token*

Prefer a mapper on the client's dedicated scope over editing the built-in `roles` client scope: the
latter is shared by every client in the realm, so a change there reaches far beyond Teamster.

If you use the shared scope anyway, it is **Client scopes → `roles` → Mappers → realm roles** (or
*client roles*) → *Add to ID token*.

## Checking what a token actually carries

When a login is refused, the message names the claim, where it looked, and the values it found, so
the mismatch is usually obvious without touching Keycloak. To see a token directly, sign in to the
realm's account console and inspect the token, or enable Keycloak's event logging and read the
`CODE_TO_TOKEN` events.

## Signing out

`POST /admin/logout` ends the Teamster session and clears the cookie. It does not call Keycloak's
`end_session_endpoint`, so the Keycloak session survives and signing back in will not prompt for a
password. Ending the Keycloak session too needs a `post_logout_redirect_uri` registered on the
client, which Teamster does not yet do.
