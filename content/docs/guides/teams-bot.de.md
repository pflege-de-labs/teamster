---
title: Den Teams-Bot einrichten
weight: 3
---

Der Teams-Bot postet und bearbeitet jede Kanal-Karte und sendet Alarme in den eigenen Chat einer
Person. Ohne ihn erreicht nichts Teams: Der Start protokolliert das, und jede Zustellung an einen
Kanal schlägt mit `502` fehl. Diese Anleitung richtet den Bot ein, verknüpft die Chats von Personen
und installiert den Bot für alle.

Der Bot ist eine zweite Entra-Registrierung, getrennt von der für `graph`. Wer die eine widerruft
oder rotiert, berührt die andere nie
([ADR 0045](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0045-channel-delivery-through-the-bot.md)).

## Den Bot konfigurieren {#configure-the-bot}

{{% steps %}}

### Den Bot registrieren {#register-the-bot}

Legen Sie einen Bot-Framework-Bot mit aktiviertem Microsoft-Teams-Kanal an und setzen Sie seinen
Messaging-Endpunkt auf `https://<external-host>/bot/messages`. Eine Entra-App-Registrierung allein
ist noch kein Bot.
[Registering the bot](https://github.com/pflege-de-labs/teamster/blob/main/manifest/README.md#registering-the-bot)
führt durch das Teams Developer Portal und die Alternative über Azure Bot.

### Teamster die Zugangsdaten geben {#give-teamster-the-credentials}

```yaml
bot:
  tenant-id: "<tenant-id>"
  client-id: "<bot-app-id>"
  tenant-type: "single"
```

Übergeben Sie das Secret als `TEAMSTER_BOT_CLIENT_SECRET`. Mit dem Helm-Chart gehören die
Einstellungen unter `config.settings.bot` und das Secret in `credentials.botClientSecret`.

Setzen Sie `bot.tenant-id`, `bot.client-id` und `bot.client-secret` gemeinsam: Sind nur einige
davon gesetzt, wird das beim Start abgelehnt. `bot.tenant-id` darf leer bleiben, wenn
`bot.tenant-type` auf `multi` steht.

### Den Tenant-Typ abgleichen {#match-the-tenant-type}

`bot.tenant-type` muss zu den „Unterstützten Kontotypen“ der Registrierung in Entra passen:

| Wert | Registrierung akzeptiert |
| --- | --- |
| `single` (Standard) | nur Benutzer dieses Tenants |
| `multi` | Benutzer jedes Tenants, authentifiziert über einen gemeinsamen Microsoft-Endpunkt |

Passen sie nicht zusammen, lässt sich die Teams-App zwar installieren, aber jede Token-Anfrage und
jede eingehende Aktivität schlägt fehl.

### `/bot/messages` für Microsoft erreichbar machen {#make-botmessages-reachable-from-microsoft}

Ist der Bot eingeschaltet, registriert Teamster `POST /bot/messages`, wohin Teams Aktivitäten
liefert. Teams muss den Endpunkt aus dem Internet erreichen. Mit dem Helm-Chart veröffentlichen Sie
ihn über den Ingress oder über `httpRoute.external`, nie nur über `httpRoute.internal`.

Der Endpunkt trägt keine Teamster-Zugangsdaten. Jede Anfrage wird gegen die Signatur von Microsoft
geprüft, anhand des Bot-Framework-Metadatendokuments unter `bot.metadata-url`. Lassen Sie diese
URL bei `https`; ist sie leer oder reines `http`, startet Teamster nicht.

### Die Teams-App paketieren und installieren {#package-and-install-the-teams-app}

Bauen Sie das App-Paket aus
[`manifest/`](https://github.com/pflege-de-labs/teamster/blob/main/manifest/README.md), ersetzen
Sie seine Platzhalter und laden Sie es im Teams Admin Center hoch. Danach fügt eine Team-Besitzerin
oder ein Teams-Administrator die App **jedem Team hinzu, in das eine Route postet**. Teamster kann
die App nicht selbst in einem Team installieren.

{{% /steps %}}

Graph findet die App über ihren Bot, also über `bot.client-id`; die eigene `id` des Manifests
spielt hier keine Rolle.

## Prüfen, wo die App installiert ist {#check-where-the-app-is-installed}

**Teams** (`/admin/teams`) listet jedes Team mit seinen Zielen und den Routen, die sie verwenden.
Teams, die von Zielen verwendet werden, denen aber die App fehlt, stehen oben, zusammen mit den
Schritten zur Installation. Auch die Team-Auswahl, die Liste der Ziele und das Routing-Bild warnen
vor einem Team ohne die App.

| Status | Bedeutung |
| --- | --- |
| **App installiert** | der Bot hat von dem Team gehört, oder Graph bestätigt, dass die App da ist |
| **App fehlt** | Graph meldet, dass die App nicht da ist |
| **Installation unbekannt** | keines von beiden kann es sagen |

Ohne `TeamsAppInstallation.ReadForTeam.All` zeigt jedes Team, das die App installiert hat, bevor
Teamster mithörte, **Installation unbekannt**. Siehe
[Microsoft-Graph-Berechtigungen erteilen](../graph-permissions/).

Wird die App einem Team hinzugefügt, speichert der Bot den regionalen Bot-Connector-Endpunkt dieses
Teams. Ein Team, von dem der Bot noch nichts gehört hat, wird über `bot.service-url` erreicht
(Standard `https://smba.trafficmanager.net/teams/`). Ein Post an ein Team ohne die App schlägt mit
einer Meldung fehl, die fragt, ob die App installiert ist.

{{< callout type="info" >}}
Noch nicht an einem echten Tenant bestätigt: Posten in private und geteilte Kanäle, Bearbeiten
einer Karte über ihre gespeicherte Conversation-ID und ob ein App-Upgrade die
Installationsereignisse erneut sendet.
{{< /callout >}}

## Den Chat einer Person verknüpfen {#link-a-persons-chat}

Eine Route kann statt an einen Kanal in den Chat einer einzelnen Person zustellen. Diese Person
verknüpft ihren Chat zuerst.

Ist [`bot.global-install`](#install-the-bot-for-everyone) eingeschaltet, entfällt das: Die
Anmeldung findet Ihren Chat über Ihr Entra-Konto, und **Benachrichtigungen** zeigt, ob das geklappt
hat. Hat der Bot noch keinen Chat mit Ihnen, installiert ihn **Meinen Chat jetzt einrichten**
([ADR 0065](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0065-your-own-chat-is-found-from-your-sign-in.md)).
Hinter Keycloak muss die Entra-Object-ID des Benutzers bei Teamster ankommen; siehe
[Keycloak konfigurieren](../keycloak/).

Andernfalls:

{{% steps %}}

### Einen Verknüpfungscode anfordern {#get-a-link-code}

Melden Sie sich mit einer beliebigen Rolle ab `viewer` an der Verwaltungsoberfläche an und öffnen
Sie **Benachrichtigungen** (`/admin/notifications`). Fordern Sie einen Code an.

Ein Skript bekommt einen über `POST /api/recipients/link` mit einer angemeldeten Sitzung:

```json
{ "code": "AB3D-EFGH-J2MN", "expires_at": "2026-09-16T10:30:00Z" }
```

Basic Auth wird auf beiden Wegen abgelehnt, weil sie jeden Aufrufer an die gemeinsame
Administrator-Anmeldung binden würde.

### Den Code an den Bot senden {#send-the-code-to-the-bot}

Öffnen Sie in Teams einen Einzelchat mit dem Bot, fügen Sie die App bei Bedarf zuerst hinzu und
senden Sie den Code. Er funktioniert auch nach einer `@`-Erwähnung des Bots.

### Auf die Bestätigung warten {#wait-for-the-confirmation}

Der Bot bestätigt im selben Chat. Ein Code ist einmal verwendbar und läuft nach zehn Minuten ab. Auf
einen abgelaufenen oder schon verwendeten Code antwortet der Bot, ohne zu sagen, was davon zutrifft.

### Eine Route auf den Chat richten {#point-a-route-at-the-chat}

Wählen Sie unter **Liefert an** der Route die Person. Ein Bearbeiter darf nur seinen eigenen Chat
wählen, ein Administrator den jeder Person. Eine Route stellt an einen Kanal oder an eine Person zu,
nie an beides.

{{% /steps %}}

Löst jemand einen Code ein, der schon verknüpft ist, gehen seine Alarme künftig in den neuen Chat,
und der alte Chat erfährt, dass er abgelöst wurde.

Im Chat einer Person bearbeitet ein wiederholtes Öffnen-Ereignis die schon vorhandene Nachricht,
und ein Schließen kommt als neue Nachricht an.

**Benachrichtigungen** zeigt, ob Ihr Chat verknüpft ist, den beim Verknüpfen erfassten
Anzeigenamen, wann die Verknüpfung entstand und wann sie zuletzt geändert wurde. Auch eine
Aktualisierung der Service-URL ändert „zuletzt geändert“; eine Änderung dort allein beweist also
keine Übernahme. **Meinen Code widerrufen** macht jeden Code ungültig, den Sie noch haben. Jede
Antwort dieser Seite wird mit `Cache-Control: no-store` gesendet.

## Mit dem Bot sprechen {#talk-to-the-bot}

Im persönlichen Chat beantwortet der Bot diese Befehle:

| Befehl | Was er tut |
| --- | --- |
| `help` | Listet die Befehle auf. |
| `status` | Zeigt, ob dieser Chat verknüpft ist, mit wem und seit wann, und welche Routen an ihn zustellen. |
| `test` | Sendet über den echten Zustellweg einen Testalarm an diesen Chat, gerendert mit der Standardvorlage des universellen Webhooks. |
| `unlink` | Beendet die Zustellung von Alarmen hierher. `stop` und `unsubscribe` tun dasselbe. |

Wählen Sie einen Befehl aus dem Menü über dem Eingabefeld oder tippen Sie ihn ein. Der Befehl muss
die ganze Nachricht sein; `/help` mit Schrägstrich funktioniert auch, aber das eigene `/`-Menü von
Teams fängt ihn womöglich vorher ab. Alles andere wird als Verknüpfungscode gelesen.

Erwähnen Sie den Bot in einem Teamkanal mit installierter App: `@Teamster help`,
`@Teamster status` oder `@Teamster test`. `status` listet die Ziele, die diesen Kanal nennen, und
die Routen, die an sie posten; `test` postet einen Testalarm in den Kanal. Beide brauchen unter
**Ziele** ein Ziel für den Kanal. Die Antwort kommt im Thread Ihrer Nachricht. Gruppenchats werden
nicht unterstützt.

Bekommt `test` keine Antwort, kann der Bot nicht senden. Prüfen Sie die Zeilen `bot reply` und
`bot test` im Log und ob `bot.tenant-type` zur Registrierung passt.

## Alarme an einen Chat beenden {#stop-alerts-to-a-chat}

Eine Person hat drei Wege hinaus:

* **`unlink` senden** (oder `stop` oder `unsubscribe`) an den Bot. Der Bot bestätigt. Ein neuer
  Verknüpfungscode verbindet wieder.
* **Die App deinstallieren.** Teams meldet das Entfernen, und die Verknüpfung erlischt von selbst.
* **In der Verwaltungsoberfläche die Verknüpfung aufheben:** den eigenen Chat unter
  **Benachrichtigungen**, den jeder Person unter **Empfänger**.

Jeder Weg entfernt den Empfänger und alle noch für ihn verfolgten Alarmkarten.

Ist `bot.global-install` eingeschaltet, funktioniert nur der letzte Weg, und nur für einen
Administrator unter **Empfänger**. Der Bot beantwortet `unlink` mit dem Hinweis, dass die IT den
Chat verwaltet, **Benachrichtigungen** hat keine Schaltfläche **Verknüpfung aufheben**, und eine
entfernte App installiert der nächste Lauf wieder
([ADR 0061](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0061-no-opt-out-when-installed-for-everyone.md)).

## Empfänger verwalten {#manage-recipients}

**Empfänger** (`/admin/recipients`) listet alle mit verknüpftem Chat: Anzeigename (oder Subject),
wann sie verknüpft haben und welche Routen an sie zustellen. Zum Ansehen genügt `viewer`; zum
Aufheben einer Verknüpfung braucht es `editor`.

Das Aufheben entfernt den Empfänger auch dann, wenn eine Route ihn noch nennt. Diese Route zeigt
dann in `/admin/routing` einen fehlenden Empfänger.

Ein dauerhafter Sendefehler, etwa bei einer Person, die den Bot deinstalliert oder blockiert hat,
markiert die Zeile als **Blockiert**, mit Grund und Zeitpunkt. Die Markierung dient nur der
Information: Der nächste Alarm wird trotzdem versucht, und ein erfolgreicher Versand hebt sie auf.

Dieselbe Liste und dieselbe Aktion zum Aufheben gibt es als `GET /api/recipients` und
`DELETE /api/recipients/{id}`.

## Den Bot für alle installieren {#install-the-bot-for-everyone}

Mit `bot.global-install` installiert Teamster die App für jedes aktive Mitglied des Tenants, Gäste
ausgenommen. Die IT kann dann jede Person erreichen, ohne dass diese einen Chat verknüpft
([ADR 0059](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0059-install-the-teams-app-for-every-member.md)).

{{% steps %}}

### Die App im Katalog der Organisation veröffentlichen {#publish-the-app-to-the-organization-catalog}

Laden Sie das App-Paket im Teams Admin Center hoch. Nur eine App im Katalog der Organisation lässt
sich über Graph installieren.

### Die Graph-Berechtigungen erteilen {#grant-the-graph-permissions}

`User.Read.All`, `TeamsAppInstallation.ReadWriteForUser.All` und, sofern Sie `bot.catalog-app-id`
nicht setzen, `AppCatalog.Read.All`. Siehe
[Microsoft-Graph-Berechtigungen erteilen](../graph-permissions/); dort steht auch, wie Sie
stattdessen über eine Setup-Richtlinie installieren.

### Einschalten {#turn-it-on}

```yaml
bot:
  global-install: true
  app-id: "<manifest-id>"
  reconcile-interval: "6h"
  welcome-message: ""
```

Setzen Sie `bot.app-id` auf die `id` des Manifests oder `bot.catalog-app-id` auf die Katalog-ID.
Ohne eines von beiden startet Teamster nicht.

### Den ersten Lauf starten {#start-the-first-run}

Öffnen Sie **Personen** (`/admin/people`, nur für Administratoren) und klicken Sie auf
**Für alle installieren**, oder rufen Sie `POST /api/people/install` auf.
`GET /api/people/runs/latest` meldet den Fortschritt.

{{% /steps %}}

Danach gilt:

* Ein Lauf alle `bot.reconcile-interval` installiert für neue Mitglieder und installiert erneut für
  alle, die die App entfernt haben. `0` läuft nur, wenn ein Administrator es anfordert; jeder andere
  Wert muss mindestens `5m` sein. Es läuft immer nur ein Replikat zugleich.
* Personen, die die App schon haben, etwa über eine Setup-Richtlinie, werden nur nachgeschlagen.
  Ohne `TeamsAppInstallation.ReadWriteForUser.All` installiert Teamster nichts und findet nur diese
  Chats.
* Eine Person, die gegangen ist, wird als ausgeschieden markiert, sobald die Mitgliederliste sie
  nicht mehr liefert, und 30 Tage später entfernt.
* Ein Opt-out gibt es nicht; siehe [Alarme an einen Chat beenden](#stop-alerts-to-a-chat).
* Der Bot begrüßt eine neue Installation mit `bot.welcome-message`, wenn der Schlüssel gesetzt ist,
  und sagt sonst nichts.

**Personen** zeigt, wie viele Personen die App haben, den letzten Lauf mit seinem Fortschritt und
fehlgeschlagene Installationen mit ihrem Grund. Ein von Hand gestarteter Lauf wiederholt sofort jede
fehlgeschlagene Installation; starten Sie also einen, nachdem Sie eine fehlende Berechtigung
erteilt haben.

Eine Route kann jetzt an **Jede in der Nachricht genannte Person** zustellen. Wer Routen bearbeiten
darf, darf eine solche Route anlegen. Siehe
[Nachrichten an einzelne Personen senden](../direct-messages/).
