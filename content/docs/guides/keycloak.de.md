---
title: Keycloak für die Anmeldung an der Verwaltung einrichten
weight: 17
---

Richten Sie einen Keycloak-Client ein, damit sich Personen mit ihrem Realm-Konto an Teamster
anmelden, ihre Rolle aus einer Keycloak-Rolle erhalten und auf Wunsch aus ihren eigenen Teams und
Kanälen wählen können.

Dies ist die Anmeldung für **Personen**. Die Entra-Zugangsdaten unter `graph.*` sind eine
Maschinen-Anmeldung zum Posten von Karten und haben damit nichts zu tun. Was auf Seiten von
Teamster einzustellen ist, beschreibt
[Anmeldung an der Verwaltungsoberfläche einrichten](../signing-in/).

## Client und Rollen anlegen {#create-the-client-and-roles}

{{% steps %}}

### Client anlegen {#create-the-client}

Legen Sie in dem Realm, der Ihre Betreiber enthält, einen Client an:

| Einstellung | Wert |
| --- | --- |
| Client ID | `teamster`, passend zu `auth.oidc-client-id` |
| Client authentication | **Off**: ein öffentlicher Client, also kein Secret, das aufzubewahren wäre |
| Authentication flow | Nur Standard flow |
| PKCE method | `S256` |
| Valid redirect URI | Die exakte absolute URL, zum Beispiel `https://teamster.example/admin/auth/callback` |

Verwenden Sie keine Redirect-URI mit Platzhalter. Ohne Client-Secret ist die Redirect-URI das
Einzige, was den Ablauf an Teamster bindet. Ein vertraulicher Client funktioniert ebenfalls: Setzen
Sie dann `auth.oidc-client-secret`.

### Rollen anlegen {#create-the-roles}

Legen Sie Rollen mit genau den Namen `admin`, `editor` und `viewer` an, als Realm-Rollen oder als
Client-Rollen am Client `teamster`, und weisen Sie sie Ihren Betreibern zu. Rollen mit anderen
Namen erreichen Teamster ebenfalls, gewähren aber nichts, bis eine Cedar-Richtlinie sie nennt;
siehe [Rollen und Zugriff verwalten](../roles/).

### Teamster auf den Realm zeigen lassen {#point-teamster-at-the-realm}

```yaml
auth:
  oidc-discovery-url: "https://<host>/realms/<realm>/.well-known/openid-configuration"
  oidc-client-id: "teamster"
  oidc-redirect-url: "https://teamster.example/admin/auth/callback"
  claim: "realm_access.roles"       # Client-Rollen: resource_access.teamster.roles
  default-role: ""                  # oder viewer
```

Ältere Keycloak-Installationen liefern das Discovery-Dokument unter `/auth/realms/<realm>/...` aus.
Teamster ruft die konfigurierte URL ab, daher funktioniert beides.

{{< callout type="warning" >}}
Eine **Client**-Rolle erscheint nicht unter `realm_access.roles`. Der falsche Claim-Pfad ist der
häufigste Weg, alle auszusperren, und dann bleibt nur die lokale Anmeldung als Weg zurück.
{{< /callout >}}

| Art der Rolle | `auth.claim` |
| --- | --- |
| Realm-Rolle | `realm_access.roles` |
| Client-Rolle am Client `teamster` | `resource_access.teamster.roles` |

### Anmelden und prüfen {#sign-in-and-check}

Melden Sie sich unter **/admin** an. **Benutzerinfo** im Menü unter Ihrem Namen listet jede Rolle,
die das Token trug. Die eigenen Realm-Rollen von Keycloak wie `offline_access` und
`default-roles-<realm>` erscheinen dort ebenfalls; keine Richtlinie erwähnt sie, daher gewähren sie
nichts.

{{% /steps %}}

Mapper müssen Sie nicht ändern. Die eingebauten Rollen-Mapper von Keycloak legen die Rollen ins
Access-Token, und Teamster liest den Claim aus dem ID-Token, dann aus Userinfo, dann aus dem
Access-Token.

## Rollen stattdessen ins ID-Token legen {#put-the-roles-in-the-id-token-instead}

Sollen die Rollen lieber im ID-Token reisen, fügen Sie dem eigenen Scope des Clients einen Mapper
hinzu:

1. **Clients → `teamster` → Client scopes → `teamster-dedicated` → Add mapper → By
   configuration**.
2. Wählen Sie **User Realm Role** oder **User Client Role** mit *Client ID* `teamster`.
3. Setzen Sie *Token Claim Name* auf `realm_access.roles` oder `resource_access.teamster.roles`,
   passend zu `auth.claim`. Keycloak liest `.` als Verschachtelung.
4. Aktivieren Sie *Multivalued* und *Add to ID token*.

Nehmen Sie den eigenen Scope des Clients statt des eingebauten Client-Scopes `roles`, den sich alle
Clients des Realms teilen.

## Gruppen anzeigen {#show-groups}

Fügen Sie dem eigenen Scope des Clients einen Mapper **Group Membership** mit dem Token-Claim-Namen
`groups` hinzu, oder setzen Sie `auth.groups-claim` auf den gewählten Namen. Lokale Gruppen können
dann diese Anbietergruppen nennen; siehe
[Rollen und Zugriff verwalten](../roles/#collect-people-in-groups).

## Den eigenen Teams-Chat jedes Benutzers finden {#find-each-users-own-teams-chat}

Mit eingeschaltetem `bot.global-install` verbindet Teamster einen angemeldeten Benutzer über seine
Entra-Objekt-ID mit seinem eigenen Teams-Chat. Die ID kommt aus dem Claim `auth.object-id-claim`
(Standard `oid`). Föderiert Keycloak die Anmeldung mit Entra, reicht es `oid` nicht von selbst
weiter:

1. Fügen Sie am Entra-Identitätsanbieter einen Mapper **Attribute Importer** mit **Claim** `oid`
   und **User Attribute Name** `entra_oid` hinzu. Der Sync-Modus `force` hält ihn aktuell.
2. Fügen Sie am eigenen Scope des Clients `teamster` einen Mapper **User Attribute** mit **User
   Attribute** `entra_oid` und **Token Claim Name** `oid` hinzu. Schalten Sie *Add to ID token* ein.

Ohne den Mapper weicht Teamster auf `preferred_username` aus, wenn er dem UPN des Benutzers
entspricht, und dann auf `email`, wenn `email_verified` wahr ist. In den meisten Mandanten genügt
das.

## Eigene Teams und Kanäle zur Auswahl anbieten {#let-admins-pick-their-own-teams-and-channels}

Mit `auth.broker.enabled` bietet die Auswahl unter **Ziele** jedem Administrator seine eigenen
Teams und Kanäle an. Dafür nutzt sie das Entra-Token, das Keycloak für seine Anmeldung gespeichert
hat. Das funktioniert nur, wenn Keycloak die Anmeldung an Entra weiterreicht.

Zwei Anmeldungen sind beteiligt, und jede fragt nach ihren eigenen Scopes. Teamster fragt Keycloak
nach `auth.oidc-scopes`; lassen Sie das unverändert. Keycloak fragt Entra nach den Scopes der
Identitätsanbieter-Verknüpfung, und dorthin gehören die Graph-Scopes.

{{% steps %}}

### Entra-Verknüpfung in Keycloak konfigurieren {#configure-the-entra-link-in-keycloak}

Unter **Identity providers →** Ihre Entra-Verknüpfung **→ Settings**:

| Einstellung | Wert |
| --- | --- |
| Store Tokens | **On** |
| Stored Tokens Readable | **On** |

Setzen Sie die Scopes der Verknüpfung, durch Leerzeichen getrennt, unter **Default Scopes** beim
Microsoft-Anbieter oder unter **Advanced → Scopes** bei einem generischen OpenID-Connect-Anbieter:

```text
openid profile email offline_access https://graph.microsoft.com/Team.ReadBasic.All https://graph.microsoft.com/Channel.ReadBasic.All
```

Mit `offline_access` kann Keycloak das gespeicherte Token erneuern. Die beiden Graph-Scopes listen
die eigenen Teams des Administrators und deren Kanäle.

### `read-token` gewähren {#grant-read-token}

Um das gespeicherte Token zu lesen, braucht das Keycloak-Konto des Administrators die Rolle
`read-token` des Clients `broker`. Stored Tokens Readable gewährt sie nur Benutzern, die sich danach
zum ersten Mal über die Verknüpfung anmelden. Bestehenden Benutzern weisen Sie sie unter
**Users → user → Role mapping** zu (nach Client `broker` filtern), über eine Gruppe oder mit einem
Hardcoded-Role-Mapper an der Verknüpfung (siehe [Als Code](#as-code)).

Ist am Client `teamster` *Full Scope Allowed* ausgeschaltet, fügen Sie die Rolle auch seinem eigenen
Scope hinzu, sonst fehlt sie im Token.

### Delegierte Berechtigungen in Entra erteilen {#grant-the-delegated-permissions-in-entra}

Die Entra-App-Registrierung hinter der Keycloak-Verknüpfung braucht die **delegierten**
Graph-Berechtigungen `Team.ReadBasic.All` und `Channel.ReadBasic.All` mit Administratorzustimmung.
Das ist meist nicht die Registrierung hinter `graph.*`, deren Anwendungsberechtigungen
[Microsoft-Graph-Berechtigungen erteilen](../graph-permissions/) beschreibt.

### Teamster konfigurieren {#configure-teamster}

```yaml
auth:
  broker:
    enabled: true
    idp-alias: "microsoft"                               # Alias der Verknüpfung in Keycloak
    token-encryption-key: "<32 random bytes, base64>"    # openssl rand -base64 32
```

Im Helm-Chart heißt der Schlüssel `credentials.brokerTokenEncryptionKey`.

### Erneut anmelden {#sign-in-again}

Jeder Administrator meldet sich bei Teamster ab und wieder an. Einem Token, das vor der Änderung
gespeichert wurde, fehlen die Scopes oder die Rolle.

{{% /steps %}}

Um einen Benutzer zu prüfen, öffnen Sie **Clients → `teamster` → Client scopes → Evaluate**, wählen
den Benutzer und sehen sich das erzeugte Access-Token an. Es sollte
`"resource_access": {"broker": {"roles": ["read-token"]}}` enthalten.

Fehlt die Rolle, protokolliert Teamster
`broker token request failed: {"errorMessage":"Client [teamster] not authorized to retrieve tokens
from identity provider [microsoft]."}`, und die Auswahl zeigt nichts.

### Als Code {#as-code}

Diese Ressourcen richten die Entra-Verknüpfung mit Store Tokens, Stored Tokens Readable und den
Graph-Scopes ein, gewähren `read-token` bei jeder Anmeldung über die Verknüpfung und behalten den
Scope `roles` am Client, der `resource_access.broker.roles` ins Access-Token bringt. Ersetzen Sie
`my-realm`, den Mandanten und die Zugangsdaten. Der Alias `microsoft` muss zu
`auth.broker.idp-alias` passen.

Sie verwenden den generischen OpenID-Connect-Anbieter, weil der Microsoft-Anbieter von Keycloak nur
wenige Identitätsanbieter-Mapper unterstützt.

{{< callout type="warning" >}}
Die Standard-Scopes eines Clients sind maßgeblich: Sie anzuwenden entfernt jeden Scope, der nicht
in der Liste steht. Übernehmen Sie zuerst die aktuellen Scopes des Clients, oder lassen Sie diese
Ressource weg, wenn `roles` schon vorhanden ist.
{{< /callout >}}

{{< tabs >}}
{{< tab name="Terraform" >}}

Für den Provider
[`keycloak/keycloak`](https://registry.terraform.io/providers/keycloak/keycloak/latest):

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

# Gewährt read-token bei jeder Anmeldung über die Verknüpfung, auch bestehenden Benutzern
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

# Nur mit full_scope_allowed = false: lässt read-token ins Token
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

Für [`provider-keycloak`](https://marketplace.upbound.io/providers/crossplane-contrib/provider-keycloak/v3.1.0)
v3.1.0 mit seinen namespace-gebundenen API-Gruppen `*.keycloak.m.crossplane.io`. Der eingebaute
Client `broker` und seine Rolle `read-token` werden beobachtet, nicht verwaltet. Wird eines davon
nie Ready, setzen Sie seine Annotation `crossplane.io/external-name` auf die UUID des Objekts.

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
# Gewährt read-token bei jeder Anmeldung über die Verknüpfung
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
# Nur mit fullScopeAllowed: false: lässt read-token ins Token
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

Mit eingeschaltetem *Full Scope Allowed* erreicht jede Rolle des Benutzers das Token: Setzen Sie es
auf `true` und lassen Sie die Ressource für den Rollen-Mapper weg.

Um `read-token` über eine Gruppe statt über den Hardcoded-Role-Mapper zu gewähren, legen Sie eine
Gruppe an, die die Rolle hält (nicht abschließend), und an der Verknüpfung einen
Hardcoded-Group-Mapper mit `syncMode` `FORCE`. Die
[Keycloak-Notizen im Repository](https://github.com/pflege-de-labs/teamster/blob/main/docs/keycloak.md#granting-read-token-through-a-group)
enthalten beide Varianten.

## Abgelehnte Anmeldung untersuchen {#troubleshoot-a-refused-login}

Eine abgelehnte Anmeldung nennt den Claim, wo Teamster gesucht hat und welche Werte es gefunden
hat. Um ein Token direkt zu sehen, prüfen Sie es in der Kontokonsole des Realms, oder schalten Sie
das Event-Logging von Keycloak ein und lesen Sie die Ereignisse `CODE_TO_TOKEN`.
