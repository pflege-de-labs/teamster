---
title: Anmeldung an der Verwaltungsoberfläche einrichten
weight: 16
---

Lassen Sie Personen sich über Ihren OpenID-Connect-Anbieter an **/admin** anmelden, behalten Sie
die lokale Anmeldung als Weg zurück hinein, und lassen Sie Administratoren auf Wunsch aus ihren
eigenen Teams und Kanälen wählen.

Die Verwaltungsoberfläche braucht eine Sitzung. Es gibt zwei Wege zu einer Sitzung:

* **Die lokale Anmeldung.** `admin.username` und `admin.password` werden unter **/admin/login**
  akzeptiert und als HTTP-Basic-Auth unter `/api`. Ohne sie startet Teamster nicht, und sie haben
  immer die Rolle `admin`.
* **Ein OIDC-Anbieter**, mit Authorization Code Flow und PKCE. Über den Zugriff entscheiden die
  Rollen in einem Claim.

## Lokale Anmeldung festlegen {#set-the-local-login}

```yaml
admin:
  username: admin
  password: <password>
```

Als Umgebungsvariablen: `TEAMSTER_ADMIN_USERNAME` und `TEAMSTER_ADMIN_PASSWORD`. Lassen Sie sie
konfiguriert, auch wenn Sie einen Anbieter hinzufügen: Sie sind der Weg hinein, wenn der Anbieter
nicht erreichbar oder der Claim falsch ist. Die lokale Anmeldung lässt sich nicht deaktivieren.

## OIDC-Anbieter hinzufügen {#add-an-oidc-provider}

{{% steps %}}

### Client registrieren {#register-a-client}

Registrieren Sie Teamster beim Anbieter. Ein öffentlicher Client mit PKCE braucht kein Secret.
Registrieren Sie die Redirect-URI exakt: `https://<teamster-host>/admin/auth/callback`. Für
Keycloak folgen Sie [Keycloak für die Anmeldung an der Verwaltung einrichten](../keycloak/).

### Teamster konfigurieren {#configure-teamster}

```yaml
auth:
  oidc-discovery-url: "https://<host>/realms/<realm>/.well-known/openid-configuration"
  oidc-client-id: "teamster"
  oidc-client-secret: ""                 # nur für einen vertraulichen Client
  oidc-redirect-url: "https://teamster.example/admin/auth/callback"
  claim: "realm_access.roles"            # der Claim mit den Rollen
  default-role: ""                       # admin, editor, viewer oder leer für keinen Zugriff
```

| Schlüssel | Umgebungsvariable | Standard |
| --- | --- | --- |
| `auth.oidc-discovery-url` | `TEAMSTER_AUTH_OIDC_DISCOVERY_URL` | leer: nur lokale Anmeldung |
| `auth.oidc-client-id` | `TEAMSTER_AUTH_OIDC_CLIENT_ID` | Pflicht bei einer Discovery-URL |
| `auth.oidc-client-secret` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` | leer: öffentlicher Client |
| `auth.oidc-redirect-url` | `TEAMSTER_AUTH_OIDC_REDIRECT_URL` | Pflicht, eine absolute URL |
| `auth.oidc-scopes` | `TEAMSTER_AUTH_OIDC_SCOPES` | `profile,email,roles` |
| `auth.claim` | `TEAMSTER_AUTH_CLAIM` | `realm_access.roles` |
| `auth.groups-claim` | `TEAMSTER_AUTH_GROUPS_CLAIM` | `groups` |
| `auth.default-role` | `TEAMSTER_AUTH_DEFAULT_ROLE` | leer |
| `auth.session-ttl` | `TEAMSTER_AUTH_SESSION_TTL` | `12h` |

Teamster startet nicht, wenn die Redirect-URL nicht absolut ist: Ein bloßer Pfad ginge wörtlich an
den Anbieter und würde dort abgelehnt.

### Rollen-Claim benennen {#name-the-roles-claim}

`auth.claim` ist ein Pfad mit Punkten. Für Keycloak ist das meist `realm_access.roles` für eine
Realm-Rolle oder `resource_access.<client>.roles` für eine Client-Rolle.

Teamster sucht den Claim im ID-Token, dann am Userinfo-Endpunkt, dann im Access-Token. Der erste
Treffer gilt. Die eingebauten Mapper von Keycloak legen die Rollen nur ins Access-Token, und das
genügt.

### Festlegen, was ein Benutzer ohne Rolle bekommt {#decide-what-a-user-without-a-role-gets}

Eine Rolle namens `admin`, `editor` oder `viewer` im Claim ist diese Rolle in Teamster. Ein
Benutzer, dessen Claim keine davon nennt, bekommt `auth.default-role`:

* `viewer` lässt alle, die der Anbieter authentifiziert, die Konfiguration lesen.
* Leer lässt sie sich ohne Zugriff anmelden. Eine Seite fordert sie auf, eine Administratorin oder
  einen Administrator um eine Rolle zu bitten. Wählen Sie das, wenn jeder im Realm den Client
  erreichen kann.

Was jede Rolle darf, steht unter [Rollen und Zugriff verwalten](../roles/).

### Anmelden {#sign-in}

Öffnen Sie **/admin** und wählen Sie **Mit dem Identitätsanbieter anmelden**. Die Kopfzeile nennt,
wer angemeldet ist und mit welcher Teamster-Rolle. **Benutzerinfo** im Menü unter dem Namen zeigt
alles, was der Anbieter gesendet hat: jede Rolle, die gewährten Scopes, die Gruppen und das Token,
in dem jede Angabe gefunden wurde.

{{% /steps %}}

Die Rolle wird bei der Anmeldung festgelegt und reist mit der Sitzung. Eine Änderung beim Anbieter
gilt, sobald sich die Person das nächste Mal anmeldet oder die Sitzung nach `auth.session-ttl`
abläuft.

**/admin/logout** beendet nur die Teamster-Sitzung. Die Sitzung beim Anbieter bleibt bestehen,
daher fragt eine erneute Anmeldung womöglich nicht nach einem Passwort.

## Eigene Teams und Kanäle zur Auswahl anbieten {#let-admins-pick-their-own-teams-and-channels}

Standardmäßig listet die Auswahl unter **Ziele** Teams und Kanäle mit Teamsters eigenen
Anwendungsberechtigungen auf. Wenn Keycloak die Anmeldung an Microsoft Entra weiterreicht, kann
Teamster jedem Administrator zusätzlich seine eigenen Teams und Kanäle anbieten, hinter dem
Umschalter **Meine Teams**. Dafür nutzt Teamster das Entra-Token, das Keycloak für diese Anmeldung
gespeichert hat, keine delegierte Registrierung von Teamster selbst.

```yaml
auth:
  broker:
    enabled: true
    idp-alias: "microsoft"                               # Alias der Entra-Verknüpfung in Keycloak
    token-encryption-key: "<32 random bytes, base64>"    # openssl rand -base64 32
```

| Schlüssel | Umgebungsvariable |
| --- | --- |
| `auth.broker.enabled` | `TEAMSTER_AUTH_BROKER_ENABLED` |
| `auth.broker.idp-alias` | `TEAMSTER_AUTH_BROKER_IDP_ALIAS` |
| `auth.broker.token-encryption-key` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` |

Das setzt `auth.oidc-discovery-url` voraus. Der Schlüssel gehört allein Teamster: Er verschlüsselt
die Keycloak-Tokens, die Teamster in seiner Datenbank speichert, und Keycloak bekommt ihn nie zu
sehen. Geben Sie jeder Replica denselben Schlüssel, und bewahren Sie ihn so dauerhaft auf wie jedes
andere Secret. Geht er verloren, sind die gespeicherten Tokens unlesbar, und die Auswahl fällt auf
die mandantenweite Liste zurück; Anmeldungen und Zustellungen sind nicht betroffen.

Eine lokale Anmeldung oder eine OIDC-Anmeldung über einen Realm ohne Entra-Föderation sieht den
Umschalter nie.

Keycloak braucht Store Tokens, Stored Tokens Readable, die Rolle `read-token` des Clients `broker`
und die delegierten Graph-Scopes. Siehe
[Keycloak einrichten: eigene Teams und Kanäle](../keycloak/#let-admins-pick-their-own-teams-and-channels)
und [ADR 0037](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0037-delegated-teams-via-keycloak-broker-token.md).
