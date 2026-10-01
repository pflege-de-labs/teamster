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
default is applied. The header shows only the Teamster role. **User info**, in the menu under the
user's name, lists all of them.

To see groups there too, add a **Group Membership** mapper to the client's dedicated scope, with
token claim name `groups`, or set `auth.groups-claim` to the name you chose. Groups are shown only
and decide nothing.

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

## Finding each user's own Teams chat

With `bot.global-install` on, Teamster binds a signed-in user to their own Teams chat by their
Entra object id instead of a link code ([ADR 0065](adr/0065-your-own-chat-is-found-from-your-sign-in.md)).
It reads the id from the claim named by `auth.object-id-claim` (default `oid`), in the id token,
userinfo or the access token.

When Keycloak federates the login against Entra, it does not pass `oid` on by itself:

1. On the Entra identity provider, add a mapper of type **Attribute Importer** with **Claim** `oid`
   and **User Attribute Name** `entra_oid`. Sync mode `force` keeps it current on every login.
2. On the Teamster client's dedicated scope, add a mapper of type **User Attribute** with **User
   Attribute** `entra_oid` and **Token Claim Name** `oid`. Turn on *Add to ID token*.

Without the mapper, Teamster falls back to `preferred_username` when it equals the user's UPN, then
to `email` when `email_verified` is true. That is enough in most tenants.

## Delegated Teams and channels

`auth-broker-enabled` lets the Destinations picker offer each admin their own Teams and channels
alongside the tenant-wide list — see [ADR 0037](adr/0037-delegated-teams-via-keycloak-broker-token.md)
for why and how. It only applies when Keycloak brokers the admin login against Microsoft Entra as
an upstream identity provider, and needs configuration in three places: the Entra IdP link in
Keycloak, the Entra app registration behind it, and Teamster itself.

Two logins are involved, and each asks for its own scopes:

* **Teamster → Keycloak** requests `auth.oidc-scopes` (default `profile,email,roles`). Leave it as
  it is: Graph scopes mean nothing to Keycloak and do not belong there.
* **Keycloak → Entra** requests the scopes configured on the identity provider link. This is where
  the Graph scopes go, because the token Keycloak stores — and hands to Teamster later — is the one
  Entra issues for this login.

### The Entra identity provider link in Keycloak

**Identity providers → Microsoft** (or your Entra OIDC/SAML link) → the realm's link → **Settings**:

| Setting | Value |
| --- | --- |
| Store Tokens | **On** |
| Stored Tokens Readable | **On** |

Store Tokens is what makes the upstream Entra token available at all after the login that produced
it finishes; Stored Tokens Readable is what lets `GET /broker/{alias}/token` hand it back out
(without it, the endpoint answers `403`). The alias in that URL — `auth-broker-idp-alias` — is the
link's own alias, shown in the identity provider list and in its settings URL.

Reading the stored token also needs the `broker` client's `read-token` role on the admin's Keycloak
account. Switching Stored Tokens Readable on grants it only to users who first sign in through the
link afterwards; assign it by hand (**Users → user → Role mapping**, filter by client `broker`), or
through a group or the realm's default roles, for everyone who already exists. If the `teamster`
client has *Full Scope Allowed* off, add the role to its dedicated scope too, or it is left out of
the token.

Without the role in the access token, Teamster logs
`broker token request failed: {"errorMessage":"Client [teamster] not authorized to retrieve tokens
from identity provider [microsoft]."}` and the picker shows nothing. [As code](#as-code) sets the
role up with Terraform or Crossplane.

Set the scopes Keycloak requests from Entra in the same link's **Scopes** field — **Default
Scopes** on the Microsoft provider, **Advanced → Scopes** on a generic OpenID Connect provider —
space-separated:

```text
openid profile email offline_access https://graph.microsoft.com/Team.ReadBasic.All https://graph.microsoft.com/Channel.ReadBasic.All
```

| Scope | Why |
| --- | --- |
| `openid`, `profile`, `email` | the login itself, as before |
| `offline_access` | an Entra refresh token, so Keycloak can renew the stored token after it expires |
| `https://graph.microsoft.com/Team.ReadBasic.All` | `GET /me/joinedTeams` — the admin's own Teams |
| `https://graph.microsoft.com/Channel.ReadBasic.All` | `GET /teams/{id}/channels` — their channels |

Keycloak sends this list at every login, so each token it stores carries the Graph scopes, not only
the one from the login where an admin happened to consent by hand. The change applies from the
next login: an admin already signed in has a stored token without the scopes and must sign out and
in again.

### The Entra app registration

The Entra application backing the Keycloak IdP link needs delegated (not application) permissions:

* `Team.ReadBasic.All`
* `Channel.ReadBasic.All`

Both need admin consent granted once for the tenant, the same as any other delegated Graph
permission. This is a different concern from `graph-*`'s application permissions, which list
Teams and channels as the app itself (see
[Microsoft Graph permissions](../README.md#microsoft-graph-permissions)) — a delegated permission
is exercised as the signed-in user, an application permission as the app itself — and the two do
not have to be, and generally are not, the same Entra app registration.

### Teamster

```yaml
auth:
  discovery-url: "https://<host>/realms/<realm>/.well-known/openid-configuration"
  broker:
    enabled: true
    idp-alias: "microsoft"                 # the Entra IdP link's own alias
    token-encryption-key: "<32 random bytes, base64>"
```

`token-encryption-key` is configured in Teamster only — nothing in Keycloak or Entra refers to it.
Teamster keeps each admin's Keycloak token in its own database, in `broker_tokens`, and this key
encrypts it there; Keycloak's copy of the Entra token is Keycloak's own business. Every replica
must be given the same key. Outside the YAML file it is
`TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY`, and in the Helm chart
`credentials.brokerTokenEncryptionKey`.

Generate the key once, e.g. `openssl rand -base64 32`, and keep it as durable as any other secret:
losing it makes every already-stored broker token permanently unreadable, which fails the delegated
picker closed — falling back to the tenant-wide list — rather than failing anyone's login or
delivery.

A local login, or an OIDC login through a realm with no Entra federation configured at all, simply
never sees the "My Teams" toggle: `auth-broker-enabled` alone does not manufacture a delegated token
where the login never produced one.

### As code

The snippets below set up everything from this section on the Keycloak side:

* the `teamster` client
* the Entra identity provider link, as a generic OpenID Connect provider with Store Tokens, Stored
  Tokens Readable and the Graph scopes
* `broker` `read-token` for everyone who signs in through that link
* the `roles` default scope, which puts `resource_access.broker.roles` into the access token

They use the generic OpenID Connect provider rather than Keycloak's Microsoft provider, which
supports few identity provider mappers. Replace `my-realm`, `teamster.example`, the tenant ID and
the credentials with your own. The alias `microsoft` must match `auth-broker-idp-alias`.

The realm's default roles stay as they are. A hardcoded role mapper on the link grants
`read-token`. With `syncMode` `FORCE` it applies at every login through the link, so users who
already exist get the role at their next login. Stored Tokens Readable alone would only reach users
created afterwards. Users who sign in another way, such as a local Keycloak account, do not get the
role. They have no stored Entra token to read anyway.

The client's default scopes are **authoritative**. Applying them removes every scope that is not in
the list, so copy the client's current scopes in first, or leave that resource out if `roles` is
already one of them.

How the role reaches the token depends on the `teamster` client's *Full Scope Allowed*. With it
off, a scope mapping lets `read-token` through. With it on, every role the user holds reaches the
token, so no mapping is needed. Each tool below has one variant for each setting.

#### Terraform

For the [`keycloak/keycloak`](https://registry.terraform.io/providers/keycloak/keycloak/latest)
provider:

```hcl
data "keycloak_realm" "realm" {
  realm = "my-realm"
}

# Built-in client that owns the read-token role
data "keycloak_openid_client" "broker" {
  realm_id  = data.keycloak_realm.realm.id
  client_id = "broker"
}

data "keycloak_role" "broker_read_token" {
  realm_id  = data.keycloak_realm.realm.id
  client_id = data.keycloak_openid_client.broker.id
  name      = "read-token"
}

locals {
  entra = "https://login.microsoftonline.com/${var.entra_tenant_id}"
}

resource "keycloak_oidc_identity_provider" "microsoft" {
  realm              = data.keycloak_realm.realm.id
  alias              = "microsoft"
  display_name       = "Microsoft"
  authorization_url  = "${local.entra}/oauth2/v2.0/authorize"
  token_url          = "${local.entra}/oauth2/v2.0/token"
  issuer             = "${local.entra}/v2.0"
  jwks_url           = "${local.entra}/discovery/v2.0/keys"
  user_info_url      = "https://graph.microsoft.com/oidc/userinfo"
  validate_signature = true
  client_id          = var.entra_client_id
  client_secret      = var.entra_client_secret
  sync_mode          = "FORCE"

  store_token                   = true # Store Tokens
  add_read_token_role_on_create = true # Stored Tokens Readable

  default_scopes = join(" ", [
    "openid", "profile", "email", "offline_access",
    "https://graph.microsoft.com/Team.ReadBasic.All",
    "https://graph.microsoft.com/Channel.ReadBasic.All",
  ])
}

# Grants read-token at every login through the link, existing users included
resource "keycloak_hardcoded_role_identity_provider_mapper" "broker_read_token" {
  realm                   = data.keycloak_realm.realm.id
  name                    = "broker-read-token"
  identity_provider_alias = keycloak_oidc_identity_provider.microsoft.alias
  role                    = "broker.read-token"

  extra_config = {
    syncMode = "FORCE"
  }
}

resource "keycloak_openid_client_default_scopes" "teamster" {
  realm_id  = data.keycloak_realm.realm.id
  client_id = keycloak_openid_client.teamster.id
  default_scopes = [
    "profile",
    "email",
    "roles",
    "web-origins",
  ]
}
```

With *Full Scope Allowed* off, the client and a scope mapping for `read-token`:

```hcl
resource "keycloak_openid_client" "teamster" {
  realm_id                     = data.keycloak_realm.realm.id
  client_id                    = "teamster"
  name                         = "Teamster"
  access_type                  = "PUBLIC"
  standard_flow_enabled        = true
  direct_access_grants_enabled = false
  pkce_code_challenge_method   = "S256"
  valid_redirect_uris          = ["https://teamster.example/admin/auth/callback"]
  full_scope_allowed           = false
}

resource "keycloak_generic_role_mapper" "teamster_broker_read_token" {
  realm_id  = data.keycloak_realm.realm.id
  client_id = keycloak_openid_client.teamster.id
  role_id   = data.keycloak_role.broker_read_token.id
}
```

With *Full Scope Allowed* on, the client alone:

```hcl
resource "keycloak_openid_client" "teamster" {
  realm_id                     = data.keycloak_realm.realm.id
  client_id                    = "teamster"
  name                         = "Teamster"
  access_type                  = "PUBLIC"
  standard_flow_enabled        = true
  direct_access_grants_enabled = false
  pkce_code_challenge_method   = "S256"
  valid_redirect_uris          = ["https://teamster.example/admin/auth/callback"]
  full_scope_allowed           = true
}
```

#### Crossplane

For [`provider-keycloak`](https://marketplace.upbound.io/providers/crossplane-contrib/provider-keycloak/v3.1.0)
v3.1.0, using its namespaced `*.keycloak.m.crossplane.io` API groups. The built-in `broker` client
and its `read-token` role are observed rather than managed. The provider finds them by realm and
client ID or role name. If either never becomes Ready, set its `crossplane.io/external-name`
annotation to the object's UUID. Replace `<tenant-id>` in the URLs.

```yaml
apiVersion: oidc.keycloak.m.crossplane.io/v1alpha2
kind: IdentityProvider
metadata:
  name: microsoft
  namespace: teamster
spec:
  forProvider:
    realm: my-realm
    alias: microsoft
    displayName: Microsoft
    authorizationUrl: https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/authorize
    tokenUrl: https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token
    issuer: https://login.microsoftonline.com/<tenant-id>/v2.0
    jwksUrl: https://login.microsoftonline.com/<tenant-id>/discovery/v2.0/keys
    userInfoUrl: https://graph.microsoft.com/oidc/userinfo
    validateSignature: true
    clientIdSecretRef:
      name: entra-idp
      key: client-id
    clientSecretSecretRef:
      name: entra-idp
      key: client-secret
    syncMode: FORCE
    storeToken: true                   # Store Tokens
    addReadTokenRoleOnCreate: true     # Stored Tokens Readable
    defaultScopes: "openid profile email offline_access https://graph.microsoft.com/Team.ReadBasic.All https://graph.microsoft.com/Channel.ReadBasic.All"
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
# Hardcoded role mapper: grants read-token at every login through the link
apiVersion: identityprovider.keycloak.m.crossplane.io/v1alpha1
kind: RoleIdentityProviderMapper
metadata:
  name: broker-read-token
  namespace: teamster
spec:
  forProvider:
    realm: my-realm
    name: broker-read-token
    identityProviderAliasRef:
      name: microsoft
    role: broker.read-token
    extraConfig:
      syncMode: FORCE
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
apiVersion: openidclient.keycloak.m.crossplane.io/v1alpha2
kind: Client
metadata:
  name: broker
  namespace: teamster
spec:
  managementPolicies: ["Observe"]
  forProvider:
    realmId: my-realm
    clientId: broker
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
apiVersion: role.keycloak.m.crossplane.io/v1alpha1
kind: Role
metadata:
  name: broker-read-token
  namespace: teamster
spec:
  managementPolicies: ["Observe"]
  forProvider:
    realmId: my-realm
    clientIdRef:
      name: broker
    name: read-token
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
apiVersion: openidclient.keycloak.m.crossplane.io/v1alpha1
kind: ClientDefaultScopes
metadata:
  name: teamster
  namespace: teamster
spec:
  forProvider:
    realmId: my-realm
    clientIdRef:
      name: teamster
    defaultScopes:
      - profile
      - email
      - roles
      - web-origins
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

With *Full Scope Allowed* off, the client and a scope mapping for `read-token`:

```yaml
apiVersion: openidclient.keycloak.m.crossplane.io/v1alpha2
kind: Client
metadata:
  name: teamster
  namespace: teamster
spec:
  forProvider:
    realmId: my-realm
    clientId: teamster
    name: Teamster
    enabled: true
    accessType: PUBLIC
    standardFlowEnabled: true
    directAccessGrantsEnabled: false
    pkceCodeChallengeMethod: S256
    validRedirectUris:
      - https://teamster.example/admin/auth/callback
    fullScopeAllowed: false
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
apiVersion: client.keycloak.m.crossplane.io/v1alpha1
kind: GenericClientRoleMapper
metadata:
  name: teamster-broker-read-token
  namespace: teamster
spec:
  forProvider:
    realmId: my-realm
    clientIdRef:
      name: teamster
    roleIdRef:
      name: broker-read-token
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

With *Full Scope Allowed* on, the client alone:

```yaml
apiVersion: openidclient.keycloak.m.crossplane.io/v1alpha2
kind: Client
metadata:
  name: teamster
  namespace: teamster
spec:
  forProvider:
    realmId: my-realm
    clientId: teamster
    name: Teamster
    enabled: true
    accessType: PUBLIC
    standardFlowEnabled: true
    directAccessGrantsEnabled: false
    pkceCodeChallengeMethod: S256
    validRedirectUris:
      - https://teamster.example/admin/auth/callback
    fullScopeAllowed: true
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

#### Granting read-token through a group

A group can hold `read-token` instead, which makes the users who have it visible under the group's
members. The group's role list is not authoritative (`exhaustive = false`). A hardcoded group mapper
on the link puts everyone who signs in through it into the group, again at every login with
`syncMode` `FORCE`. Use these resources in place of the hardcoded role mapper above, not alongside
it. Everything else stays.

Terraform:

```hcl
resource "keycloak_group" "teamster_broker_token" {
  realm_id = data.keycloak_realm.realm.id
  name     = "teamster-broker-token"
}

resource "keycloak_group_roles" "teamster_broker_token" {
  realm_id   = data.keycloak_realm.realm.id
  group_id   = keycloak_group.teamster_broker_token.id
  role_ids   = [data.keycloak_role.broker_read_token.id]
  exhaustive = false
}

resource "keycloak_hardcoded_group_identity_provider_mapper" "teamster_broker_token" {
  realm                   = data.keycloak_realm.realm.id
  name                    = "teamster-broker-token"
  identity_provider_alias = keycloak_oidc_identity_provider.microsoft.alias
  group                   = keycloak_group.teamster_broker_token.name

  extra_config = {
    syncMode = "FORCE"
  }
}
```

Crossplane:

```yaml
apiVersion: group.keycloak.m.crossplane.io/v1alpha1
kind: Group
metadata:
  name: teamster-broker-token
  namespace: teamster
spec:
  forProvider:
    realmId: my-realm
    name: teamster-broker-token
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
apiVersion: group.keycloak.m.crossplane.io/v1alpha1
kind: Roles
metadata:
  name: teamster-broker-token
  namespace: teamster
spec:
  forProvider:
    realmId: my-realm
    groupIdRef:
      name: teamster-broker-token
    roleIdsRefs:
      - name: broker-read-token
    exhaustive: false
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
---
apiVersion: identityprovider.keycloak.m.crossplane.io/v1alpha1
kind: GroupIdentityProviderMapper
metadata:
  name: teamster-broker-token
  namespace: teamster
spec:
  forProvider:
    realm: my-realm
    name: teamster-broker-token
    identityProviderAliasRef:
      name: microsoft
    group: teamster-broker-token
    extraConfig:
      syncMode: FORCE
  providerConfigRef:
    kind: ClusterProviderConfig
    name: default
```

To give the token to only some Entra users, leave out the mapper and fill the group with an explicit
member list: `keycloak_group_memberships` or `Memberships`. Both are authoritative and keyed by
username.

After any of these apply, each admin signs out of Teamster and back in. The Keycloak token Teamster
stored in `broker_tokens` was issued without the role. To check, open **Clients → teamster → Client
scopes → Evaluate**, pick a user and look at the generated access token. It should carry
`"resource_access": {"broker": {"roles": ["read-token"]}}`.

## Signing out

`POST /admin/logout` ends the Teamster session and clears the cookie. It does not call Keycloak's
`end_session_endpoint`, so the Keycloak session survives and signing back in will not prompt for a
password. Ending the Keycloak session too needs a `post_logout_redirect_uri` registered on the
client, which Teamster does not yet do.
