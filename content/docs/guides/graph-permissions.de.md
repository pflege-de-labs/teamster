---
title: Microsoft-Graph-Berechtigungen erteilen
weight: 2
---

Teamster spricht über zwei getrennte Entra-App-Registrierungen mit Microsoft. Diese Anleitung
zeigt, welche Berechtigungen jede braucht, was jede Berechtigung über Teamsters eigenen Bedarf
hinaus erreicht und wie Sie mit weniger auskommen.

## Die beiden Registrierungen kennen {#know-the-two-registrations}

| Identität | Konfiguriert über | Token-Audience | Graph-Berechtigungen |
| --- | --- | --- | --- |
| Graph-Registrierung | `graph.*` | `https://graph.microsoft.com/.default` | Anwendungsberechtigungen, siehe unten |
| Bot-Registrierung | `bot.*` | `https://api.botframework.com/.default` | keine |
| Keycloak-Broker-Token (optional) | `auth.broker.*` | Microsoft Graph | delegiert `Team.ReadBasic.All`, `Channel.ReadBasic.All` |

Beide Registrierungen melden sich mit einem Client Secret über den Client-Credentials-Flow an.
Zertifikate und föderierte Anmeldeinformationen werden nicht unterstützt.

Die Bot-Registrierung hat keine Graph-Berechtigungen. Sie postet in jedes Team und schreibt jeder
Person, sofern dort die Teams-App von Teamster installiert ist, und bearbeitet, was sie gepostet
hat. Richten Sie sie mit [Den Teams-Bot einrichten](../teams-bot/) ein. Das
Keycloak-Broker-Token ist das eigene Token des angemeldeten Administrators; siehe
[Keycloak konfigurieren](../keycloak/).

Halten Sie die beiden Registrierungen getrennt. Dann lässt sich jedes Secret widerrufen und
rotieren, ohne das andere zu berühren.
[ADR 0066](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0066-keep-separate-graph-and-bot-registrations.md)
hält fest, warum, und welche Alternative mit nur einer Registrierung geprüft wurde.

## Die Anwendungsberechtigungen erteilen {#grant-the-application-permissions}

Die Graph-Registrierung meldet sich als die Anwendung selbst an und braucht daher
**Anwendungsberechtigungen** mit Administratorzustimmung für den Tenant. Erteilen Sie die für die
Funktionen, die Sie nutzen:

| Berechtigung | Gebraucht für | Erreicht über Teamsters Bedarf hinaus | Ohne sie |
| --- | --- | --- | --- |
| `Team.ReadBasic.All` | Team-Auswahl; Teamnamen im Routing-Bild, in Export und Import | Name und Beschreibung jedes Teams im Tenant | Team-IDs werden von Hand eingetippt und statt der Namen angezeigt |
| `Channel.ReadBasic.All` | Kanalauswahl; Kanalnamen an denselben Stellen | Name und Beschreibung jedes Kanals in jedem Team | Kanal-IDs werden von Hand eingetippt und statt der Namen angezeigt |
| `TeamsAppInstallation.ReadForTeam.All` (optional) | Installationsstatus eines Teams, von dem der Bot noch nichts gehört hat | die Liste der in jedem Team installierten Apps | ein solches Team zeigt **Installation unbekannt** |
| `User.Read.All` (mit `bot.global-install`) | Auflisten der Mitglieder des Tenants, Finden einer Person per UPN oder E-Mail | das vollständige Profil jedes Benutzers | `bot.global-install` kann nicht laufen |
| `TeamsAppInstallation.ReadWriteForUser.All` (mit `bot.global-install`, optional) | Installieren der Teamster-App für jedes Mitglied | Installieren, Aktualisieren und Entfernen *jeder* Katalog-App für jeden Benutzer; keine Zustimmung zu den ressourcenspezifischen Berechtigungen dieser App | nichts wird installiert; nur Personen, die die App schon haben, werden gefunden |
| `AppCatalog.Read.All` (mit `bot.global-install`, sofern `bot.catalog-app-id` nicht gesetzt ist) | Finden der Teamster-App im Katalog der Organisation | jede App im Katalog der Organisation | setzen Sie stattdessen `bot.catalog-app-id` |

{{< callout type="warning" >}}
Rollen stecken fest im Access-Token. Starten Sie Teamster nach dem Erteilen oder Widerrufen der
Zustimmung neu, sonst behält es das alte Token bis zu einer Stunde.
{{< /callout >}}

### Die richtige Variante wählen {#pick-the-right-variant}

* `User.ReadBasic.All` genügt für `bot.global-install` nicht. Damit lassen sich `accountEnabled` und
  `userType` weder lesen noch filtern, und Graph beantwortet die Mitgliederliste mit
  `403 Authorization_RequestDenied`.
* Erteilen Sie die tenantweiten `TeamsAppInstallation`-Berechtigungen, nicht die `Self`-Varianten,
  die Graph in einer Ablehnung womöglich nennt. Die `Self`-Varianten decken nur eine Teams-App ab,
  die an die Graph-Registrierung gebunden ist, und die App von Teamster gehört zur
  Bot-Registrierung.

### Keine Berechtigungen zum Posten erteilen {#do-not-grant-posting-permissions}

Graph lässt eine Anwendung keine Kanalnachrichten posten oder bearbeiten, deshalb erledigt der Bot
beides. Erteilen Sie für Teamster nicht:

* `Teamwork.Migrate.All` ist der einzige Weg, als Anwendung zu posten, und dient dem Import von
  Nachrichtenverläufen.
* `ChannelMessage.UpdatePolicyViolation.All` ist der einzige Weg, als Anwendung zu bearbeiten, und
  darf nur das Feld `policyViolation` einer Nachricht ändern.

## Wissen, was ein geleaktes Secret preisgibt {#know-what-a-leaked-secret-exposes}

| Secret | Ein Angreifer kann |
| --- | --- |
| `graph.client-secret` | lesen, was die erteilten Berechtigungen erlauben; mit der Installationsberechtigung Katalog-Apps auf Benutzer verteilen. Er kann keine Nachricht posten oder bearbeiten. |
| `bot.client-secret` | als Bot in jedem Team posten und bearbeiten und jeder Person schreiben, die die App hat. Er kann weder das Verzeichnis lesen noch etwas installieren. |

## Mit weniger Berechtigungen auskommen {#run-with-fewer-permissions}

Jede dieser Maßnahmen wirkt für sich allein.

1. **Erteilen Sie der Graph-Registrierung nichts.** Teamster verlangt weiterhin, dass `graph.*`
   gesetzt ist, aber die Zustellung braucht keine Berechtigung: Der Bot postet alles. Die
   Auswahllisten fallen auf von Hand eingetippte IDs zurück, und Routing-Bild, Export und Import
   zeigen IDs statt Namen.
2. **Verzichten Sie auf `TeamsAppInstallation.ReadForTeam.All`.** Der Installationsstatus kommt dann
   aus den Installationsereignissen des Bots. Ein Team, das die App installiert hat, bevor der Bot
   mithörte, zeigt **Installation unbekannt**, bis es dem Bot ein Ereignis sendet.
3. **Setzen Sie `bot.catalog-app-id`** und verzichten Sie auf `AppCatalog.Read.All`. Das Teams
   Admin Center zeigt die Katalog-ID, oder Sie fragen Graph einmal:
   `GET /appCatalogs/teamsApps?$filter=externalId eq '<manifest-id>'`.
4. **Installieren Sie über eine Teams-App-Setup-Richtlinie**, statt
   `TeamsAppInstallation.ReadWriteForUser.All` zu erteilen. `bot.global-install` findet dann nur die
   Chats der Personen, für die die Richtlinie die App installiert hat, und `/admin/people` zeigt die
   übrigen als fehlgeschlagen. `User.Read.All` bleibt nötig, um Mitglieder aufzulisten.
5. **Erteilen Sie die Installationsberechtigung nur für den Rollout.** Führen Sie
   **Für alle installieren** einmal aus, widerrufen Sie
   `TeamsAppInstallation.ReadWriteForUser.All` und starten Sie neu. Spätere Läufe finden nur noch
   Chats; neue Mitglieder brauchen die Setup-Richtlinie oder eine weitere befristete Berechtigung.
6. **Lassen Sie `bot.global-install` aus**, wenn Personen ihren Chat selbst verknüpfen.
   `User.Read.All`, `TeamsAppInstallation.ReadWriteForUser.All` und `AppCatalog.Read.All` werden
   dann nicht gebraucht.
7. **Delegierte Auswahllisten** über den Keycloak-Broker ergänzen die Auswahllisten um die eigenen
   Teams und Kanäle jedes Administrators. Sie ersetzen die tenantweite Liste nicht; ohne
   `Team.ReadBasic.All` oder `Channel.ReadBasic.All` geht diese verloren.
8. **Außerhalb von Teamster:** kurze Laufzeiten der Secrets, Rotation der beiden Secrets nach
   getrennten Zeitplänen und, mit Entra Workload ID, bedingter Zugriff, der einschränkt, von wo die
   Token jeder Registrierung angefordert werden dürfen.

### Ein Profil wählen {#choose-a-profile}

| Profil | Berechtigungen der Graph-Registrierung |
| --- | --- |
| Minimal: Kanäle und verknüpfte Chats, IDs von Hand eingetippt | keine |
| Kanäle mit Auswahllisten | `Team.ReadBasic.All`, `Channel.ReadBasic.All` |
| Globale Installation, Apps über eine Setup-Richtlinie installiert | die obigen und `User.Read.All`; `AppCatalog.Read.All` dient nur einer Installation und kann daher ebenfalls entfallen |
| Maximal | jede Berechtigung aus der Tabelle oben |
