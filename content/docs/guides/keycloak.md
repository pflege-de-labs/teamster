---
title: Configure Keycloak for the admin login
weight: 18
---

Set up a Keycloak client so people sign in to Teamster with their realm account, get their role
from a Keycloak role, and, optionally, pick from their own Teams and channels.

This is the login for **people**. The Entra credentials under `graph.*` are a machine credential
for posting cards and are unrelated. The Teamster side is described in
[Configure sign-in to the admin UI](../signing-in/).

## Create the client and roles

{{% steps %}}

### Create the client

In the realm that holds your operators, create a client:

| Setting | Value |
| --- | --- |
| Client ID | `teamster`, matching `auth.oidc-client-id` |
| Client authentication | **Off**: a public client, so there is no secret to store |
| Authentication flow | Standard flow only |
| PKCE method | `S256` |
| Valid redirect URI | The exact absolute URL, for example `https://teamster.example/admin/auth/callback` |

Do not use a wildcard redirect URI. Without a client secret, the redirect URI is the only thing
binding the flow to Teamster. A confidential client works too: set `auth.oidc-client-secret`.

### Create the roles

Create roles named exactly `admin`, `editor` and `viewer`, as realm roles or as client roles on the
`teamster` client, and assign them to your operators. Roles with other names reach Teamster too,
but grant nothing until a Cedar policy names them; see [Manage roles and access](../roles/).

### Point Teamster at the realm

```yaml
auth:
  oidc-discovery-url: "https://<host>/realms/<realm>/.well-known/openid-configuration"
  oidc-client-id: "teamster"
  oidc-redirect-url: "https://teamster.example/admin/auth/callback"
  claim: "realm_access.roles"       # client roles: resource_access.teamster.roles
  default-role: ""                  # or viewer
```

Older Keycloak deployments serve the discovery document under `/auth/realms/<realm>/...`. Teamster
fetches the URL you configure, so either works.

{{< callout type="warning" >}}
A **client** role does not appear under `realm_access.roles`. The wrong claim path is the most
common way to lock everyone out, and the local login is then the only way back in.
{{< /callout >}}

| Role kind | `auth.claim` |
| --- | --- |
| Realm role | `realm_access.roles` |
| Client role on the `teamster` client | `resource_access.teamster.roles` |

### Sign in and check

Sign in at **/admin**. **User info**, in the menu under your name, lists every role the token
carried. Keycloak's own realm roles, such as `offline_access` and `default-roles-<realm>`, appear
there too; no policy mentions them, so they grant nothing.

{{% /steps %}}

No mapper changes are needed. Keycloak's built-in role mappers put the roles in the access token,
and Teamster reads the claim from the ID token, then userinfo, then the access token.

## Put the roles in the ID token instead

If you would rather the roles travel in the ID token, add a mapper to the client's dedicated scope:

1. **Clients → `teamster` → Client scopes → `teamster-dedicated` → Add mapper → By
   configuration**.
2. Choose **User Realm Role**, or **User Client Role** with *Client ID* `teamster`.
3. Set *Token Claim Name* to `realm_access.roles` or `resource_access.teamster.roles`, matching
   `auth.claim`. Keycloak reads `.` as nesting.
4. Enable *Multivalued* and *Add to ID token*.

Prefer the dedicated scope over the built-in `roles` client scope, which every client in the realm
shares.

## Show groups

Add a **Group Membership** mapper to the client's dedicated scope with token claim name `groups`,
or set `auth.groups-claim` to the name you chose. Local groups can then name these provider groups;
see [Manage roles and access](../roles/#collect-people-in-groups).

## Find each user's own Teams chat

With `bot.global-install` on, Teamster binds a signed-in user to their own Teams chat by their
Entra object id, read from the claim `auth.object-id-claim` (default `oid`). When Keycloak federates
the login against Entra, it does not pass `oid` on by itself:

1. On the Entra identity provider, add an **Attribute Importer** mapper with **Claim** `oid` and
   **User Attribute Name** `entra_oid`. Sync mode `force` keeps it current.
2. On the `teamster` client's dedicated scope, add a **User Attribute** mapper with **User
   Attribute** `entra_oid` and **Token Claim Name** `oid`. Turn on *Add to ID token*.

Without the mapper, Teamster falls back to `preferred_username` when it equals the user's UPN, then
to `email` when `email_verified` is true. That is enough in most tenants.

## Let admins pick their own Teams and channels

With `auth.broker.enabled`, the Destinations picker offers each admin their own Teams and channels,
using the Entra token Keycloak stored for their login. This works only when Keycloak brokers the
login against Entra.

Two logins are involved, and each asks for its own scopes. Teamster asks Keycloak for
`auth.oidc-scopes`; leave that alone. Keycloak asks Entra for the scopes on the identity provider
link, and that is where the Graph scopes go.

{{% steps %}}

### Configure the Entra link in Keycloak

On **Identity providers →** your Entra link **→ Settings**:

| Setting | Value |
| --- | --- |
| Store Tokens | **On** |
| Stored Tokens Readable | **On** |

Set the link's scopes, under **Default Scopes** on the Microsoft provider or **Advanced → Scopes**
on a generic OpenID Connect provider, space-separated:

```text
openid profile email offline_access https://graph.microsoft.com/Team.ReadBasic.All https://graph.microsoft.com/Channel.ReadBasic.All
```

`offline_access` lets Keycloak renew the stored token. The two Graph scopes list the admin's own
Teams and their channels.

### Grant `read-token`

Reading the stored token needs the `broker` client's `read-token` role on the admin's Keycloak
account. Stored Tokens Readable grants it only to users who first sign in through the link
afterwards. For existing users, assign it under **Users → user → Role mapping** (filter by client
`broker`), through a group, or with a hardcoded role mapper on the link (see
[As code](#as-code)).

If the `teamster` client has *Full Scope Allowed* off, add the role to its dedicated scope too, or
it is left out of the token.

### Grant the delegated permissions in Entra

The Entra app registration behind the Keycloak link needs the **delegated** Graph permissions
`Team.ReadBasic.All` and `Channel.ReadBasic.All`, with admin consent. This is usually not the
registration behind `graph.*`, whose application permissions are described in
[Grant Microsoft Graph permissions](../graph-permissions/).

### Configure Teamster

```yaml
auth:
  broker:
    enabled: true
    idp-alias: "microsoft"                               # the link's alias in Keycloak
    token-encryption-key: "<32 random bytes, base64>"    # openssl rand -base64 32
```

In the Helm chart the key is `credentials.brokerTokenEncryptionKey`.

### Sign in again

Each admin signs out of Teamster and back in. A token stored before the change lacks the scopes or
the role.

{{% /steps %}}

To check a user, open **Clients → `teamster` → Client scopes → Evaluate**, pick the user and look at
the generated access token. It should carry
`"resource_access": {"broker": {"roles": ["read-token"]}}`.

If the role is missing, Teamster logs
`broker token request failed: {"errorMessage":"Client [teamster] not authorized to retrieve tokens
from identity provider [microsoft]."}` and the picker shows nothing.

### As code

These resources set up the Entra link with Store Tokens, Stored Tokens Readable and the Graph
scopes, grant `read-token` at every login through the link, and keep the `roles` scope on the
client, which puts `resource_access.broker.roles` into the access token. Replace `my-realm`, the
tenant and the credentials. The alias `microsoft` must match `auth.broker.idp-alias`.

They use the generic OpenID Connect provider, because Keycloak's Microsoft provider supports few
identity provider mappers.

{{< callout type="warning" >}}
A client's default scopes are authoritative: applying them removes every scope not in the list.
Copy the client's current scopes in first, or leave that resource out if `roles` is already there.
{{< /callout >}}

{{< tabs >}}
{{< tab name="Terraform" >}}

For the [`keycloak/keycloak`](https://registry.terraform.io/providers/keycloak/keycloak/latest)
provider:

```hcl
data "keycloak_realm" "realm" {
  realm = "my-realm"
}

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

# Only with full_scope_allowed = false: lets read-token into the token
resource "keycloak_generic_role_mapper" "teamster_broker_read_token" {
  realm_id  = data.keycloak_realm.realm.id
  client_id = keycloak_openid_client.teamster.id
  role_id   = data.keycloak_role.broker_read_token.id
}

resource "keycloak_openid_client_default_scopes" "teamster" {
  realm_id       = data.keycloak_realm.realm.id
  client_id      = keycloak_openid_client.teamster.id
  default_scopes = ["profile", "email", "roles", "web-origins"]
}
```

{{< /tab >}}
{{< tab name="Crossplane" >}}

For [`provider-keycloak`](https://marketplace.upbound.io/providers/crossplane-contrib/provider-keycloak/v3.1.0)
v3.1.0, with its namespaced `*.keycloak.m.crossplane.io` API groups. The built-in `broker` client
and its `read-token` role are observed, not managed. If either never becomes Ready, set its
`crossplane.io/external-name` annotation to the object's UUID.

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
# Grants read-token at every login through the link
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
# Only with fullScopeAllowed: false: lets read-token into the token
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

{{< /tab >}}
{{< /tabs >}}

With *Full Scope Allowed* on, every role the user holds reaches the token: set it to `true` and
drop the role mapper resource.

To grant `read-token` through a group instead of the hardcoded role mapper, create a group holding
the role (non-exhaustively) and a hardcoded group mapper on the link with `syncMode` `FORCE`. The
[Keycloak notes in the repository](https://github.com/pflege-de-labs/teamster/blob/main/docs/keycloak.md#granting-read-token-through-a-group)
have both variants.

## Troubleshoot a refused login

A refused login names the claim, where Teamster looked and the values it found. To see a token
directly, inspect it in the realm's account console, or enable Keycloak's event logging and read
the `CODE_TO_TOKEN` events.
