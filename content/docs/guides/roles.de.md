---
title: Rollen und Zugriff verwalten
weight: 19
---

Geben Sie Personen eine Rolle, fassen Sie sie in Gruppen zusammen, lassen Sie sie die Webhooks
verwenden, teilen Sie einzelne Datensätze mit ihnen, und beschränken Sie eine Rolle auf bestimmte
Teams und Kanäle.

Wie Rollen, Eigentümer und Cedar-Richtlinien zusammenspielen, erklärt
[Eigentum und Freigabe](../../concepts/ownership/). Diese Seite enthält die Aufgaben.

## Jemandem eine Rolle geben {#give-someone-a-role}

Rollen kommen aus Ihrem Identitätsanbieter, eins zu eins nach Namen. Weisen Sie eine Anbieterrolle
namens `admin`, `editor` oder `viewer` zu, und der Benutzer hat diese Rolle bei seiner nächsten
Anmeldung.

| Rolle | Darf |
| --- | --- |
| `admin` (**Administrator**) | Alles, auch alles, was spätere Releases hinzufügen |
| `editor` (**Bearbeiter**) | Die Konfiguration lesen und ändern |
| `viewer` (**Betrachter**) | Sie lesen und den eigenen Chat verknüpfen oder die Verknüpfung aufheben |

Ein Benutzer mit mehreren Rollen hat alle davon. Ein Benutzer ohne eine der drei bekommt
`auth.default-role`; das kann `admin`, `editor`, `viewer` oder leer für keinen Zugriff sein. Siehe
[Anmeldung einrichten](../signing-in/#decide-what-a-user-without-a-role-gets).

Die Rolle wird bei der Anmeldung festgelegt. Eine Änderung beim Anbieter gilt, sobald sich die
Person das nächste Mal anmeldet.

### Eigene Rolle definieren {#define-a-role-of-your-own}

Eine Rolle mit einem anderen Namen, zum Beispiel `auditor`, erreicht Teamster unter ihrem eigenen
Namen und gewährt nichts, bis eine Cedar-Richtlinie sie nennt. Die Richtlinien sind in das Binary
eingebettet, in
[`internal/authz/policies.cedar`](https://github.com/pflege-de-labs/teamster/blob/main/internal/authz/policies.cedar).
Eine eigene Rolle heißt also, dort eine Richtlinie für `Role::"auditor"` hinzuzufügen und Teamster
zu bauen. Richtlinien hinzuzufügen ist unbedenklich. Wer `admin`, `editor` oder `viewer` entfernt
oder umbenennt, macht die Verwaltungsoberfläche unbrauchbar.

## Prüfen, wer was darf {#check-who-may-do-what}

* **Zugriffsübersicht** (**/admin/access**) erscheint im Menü für Administratoren und für alle, die
  eine Berechtigung nennt, direkt oder über eine Gruppe, Anbietergruppe oder Rolle. Dort
  [teilen sie mehrere Datensätze auf einmal](#share-several-records-at-once), und **Alle
  Berechtigungen** listet die Berechtigungen auf den Datensätzen, die sie teilen dürfen; der Name
  jedes Datensatzes verlinkt auf ihn.
* Administratoren sehen dort außerdem, wer an die Webhooks senden darf, wer Personen anschreiben
  darf, jede Berechtigung, **Wer darf?** und die **Geltenden Richtlinien**. **Wer darf?** nimmt ein
  Subject entgegen, dazu eine Aktion und eine Ressource aus Listen, die auch die Webhooks und
  **Personen** enthalten, und nennt die Richtlinien, die sie erlauben oder verweigern. Den Text einer
  Richtlinie klappen Sie unter ihrem Namen auf. Ältere Links mit `type=` und `id=` funktionieren
  weiterhin.
* **Mein Zugriff** im Kontomenü (**/admin/me**) zeigt jedem die eigenen Rollen, Gruppen, Webhooks
  und **Wen Sie anschreiben dürfen**. **Mit Ihnen geteilt** listet jeden mit ihm geteilten
  Datensatz, mit Link, den Aktionen und **Über** wen: sich selbst, eine Gruppe, eine Anbietergruppe
  oder eine Rolle. Darunter sagt je Rolle eine Zeile, was sie auf allen Datensätzen einer Art
  erlaubt. Die zugrunde liegenden Cedar-Richtlinien stehen unter **Die zugrunde liegenden
  Richtlinien anzeigen**.

Eine verweigerte Anfrage ist ein `403`, das die Rolle und die Ressource nennt.

## Personen in Gruppen zusammenfassen {#collect-people-in-groups}

Gruppen unter **/admin/groups** fassen Benutzer, andere Gruppen und die Gruppen zusammen, die Ihr
Identitätsanbieter in `auth.groups-claim` (Standard `groups`) nennt. Bearbeiter legen Gruppen an
und ändern sie, sehen können sie alle. Gruppen lassen sich verschachteln, aber keine Gruppe kann
sich selbst enthalten.

Berechtigungen auf Datensätze und Webhook-Stufen lassen sich einer Gruppe genauso erteilen wie
einem Benutzer. Jede Änderung einer Mitgliedschaft wird in derselben Transaktion im
[Änderungsprotokoll](../audit-trail/) festgehalten.

```bash
# Gruppe anlegen, dann eine Anbietergruppe hinzufügen
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/groups -d '{"name": "payments"}'
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/groups/<group id>/members \
  -d '{"type": "idp_group", "id": "/payments-oncall"}'
```

Der `type` eines Mitglieds ist `user`, `group` oder `idp_group`. Gruppen gehören nicht zu einem
Konfigurationsbündel.

## Jemanden die Webhooks verwenden lassen {#let-someone-use-the-webhooks}

Bearbeiter und Administratoren dürfen beide Webhooks verwenden. Alle anderen brauchen eine
Webhook-Stufe, die auf **/admin/access** je Benutzer, Gruppe, Anbietergruppe oder Rolle gesetzt
wird:

| Stufe | API-Wert | Webhooks |
| --- | --- | --- |
| keine | `none` | Keine |
| Alertmanager | `alertmanager` | `/webhook/alertmanager` |
| Universal | `universal` | `/webhook/universal` |
| beide Webhooks | `all` | Beide |
| beide, und Token verwalten | `admin` | Beide, dazu die Verwaltung der Token aller |

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/access/webhooks \
  -d '{"principal_type": "group", "principal_id": "<group id>", "level": "alertmanager"}'
```

`principal_type` ist `user`, `group`, `idp_group` oder `role`. Mit einer Stufe können sie eigene
Token ausstellen; siehe [Webhook-Absender authentifizieren](../webhook-tokens/). Wird ihnen die
Stufe entzogen, sind diese Token bei ihrer nächsten Verwendung widerrufen.

## Jemanden Personen anschreiben lassen {#let-someone-message-people}

Eine Nachricht, die Personen nennt, braucht ein Token, das sie nennen darf; siehe
[Ein Token Personen nennen lassen](../webhook-tokens/#let-a-token-name-people). Jede angemeldete
Person darf sich selbst nennen. Andere zu nennen erfordert die Stufe **beliebige**, gesetzt auf
**/admin/access** unter **Wer Personen anschreiben darf**, je Benutzer, Gruppe, Anbietergruppe
oder Rolle. Administratoren haben sie bereits. Erteilen Sie sie den Personen, die Hinweise an
Kollegen versenden, etwa Office-Administratoren. Die Stufe **beliebige, und Rundsendung an alle**
erlaubt zusätzlich, eine Nachricht an alle zu senden, die der Bot erreichen kann; siehe
[Eine Nachricht an alle senden](../broadcasts/).

| Stufe | API-Wert | Sie und ihre Token dürfen nennen |
| --- | --- | --- |
| nur sich selbst | `none` | Nur sich selbst |
| beliebige | `anyone` | Jede Person im Mandanten |
| beliebige, und Rundsendung an alle | `everyone` | Jede Person, und [alle auf einmal](../broadcasts/) |

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/access/messages \
  -d '{"principal_type": "idp_group", "principal_id": "<provider group>", "level": "anyone"}'
```

`principal_type` ist `user`, `group`, `idp_group` oder `role`. `none` nimmt die Stufe zurück, und
ihre Token mit **beliebige** oder **beliebige, und Rundsendung** nennen ab der nächsten Anfrage nur
noch ihren Ersteller. In Cedar ist **beliebige** die Aktion `message` auf `People::"*"` und
**beliebige, und Rundsendung an alle** die Aktion `broadcast`. Jede schließt die Stufen darunter
ein, bis hinab zu `messageSelf`, der Aktion, die jeder hat. Siehe
[ADR 0082](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0082-naming-people-takes-permission.md)
und
[ADR 0083](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0083-broadcasts-run-in-the-background.md).

## Einen Datensatz teilen {#share-a-record}

Wer eine Vorlage, ein Ziel, eine Route, einen Webhook-Endpunkt oder eine Gruppe anlegt, ist ihr
Eigentümer. Um einen Datensatz zu teilen, bearbeiten Sie ihn und nutzen **Wer was darf**:

1. Wählen Sie unter **Geben an** einen Benutzer, eine Gruppe, eine Anbietergruppe oder eine Rolle
   aus der Liste. Wer sich noch nicht angemeldet hat, steht nicht darin: Öffnen Sie **Nicht
   aufgeführte Person**, wählen Sie die Art und geben Sie stattdessen das Subject, den Namen der
   Anbietergruppe oder der Rolle ein.
2. Haken Sie an, was er **Darf**, und klicken Sie auf **Speichern**.

| Aktion | Im Formular | Erlaubt |
| --- | --- | --- |
| `read` | lesen | Den Datensatz sehen |
| `update` | ändern | Ihn ändern |
| `delete` | löschen | Ihn löschen |
| `attach` | in Routen und Webhooks verwenden | Routen und Webhooks auf ihn zeigen lassen |
| `share` | teilen | Anderen geben, was man selbst hat |
| `own` | besitzen (alles, auch Eigentum weitergeben) | Alles, auch das Eigentum weitergeben |

Wer schon etwas hat, steht als Zeile mit seinen angehakten Aktionen darunter. Ändern Sie die
Häkchen und klicken Sie auf **Speichern**, um zu ersetzen, was er hat; ohne Häkchen wird ihm alles
entzogen. **Entziehen** entfernt die Zeile.

Niemand kann mehr vergeben, als er selbst hat. Wer keine Rolle, aber einen geteilten Datensatz hat,
sieht **/admin** nur mit dem, was geteilt wurde.

`create` auf eine ganze Sammlung wird über die API erteilt:

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/sharing \
  -d '{"principal_type": "group", "principal_id": "<group id>",
       "resource_type": "Route", "resource_id": "*", "actions": ["create"]}'
```

`GET /api/sharing?type=Template&id=<id>` listet die Berechtigungen eines Datensatzes,
`DELETE /api/sharing/<id>` widerruft eine.

## Mehrere Datensätze auf einmal teilen {#share-several-records-at-once}

{{% steps %}}

### Art der Ressource wählen {#pick-a-kind-of-resource}

Öffnen Sie die **Zugriffsübersicht** (**/admin/access**), wählen Sie eine **Art der Ressource**
(Vorlagen, Ziele, Routen, Webhook-Endpunkte oder Gruppen) und klicken Sie auf **Anzeigen**.
Aufgeführt sind nur Datensätze, die Sie teilen (`share`) dürfen. Gibt es keine, sagt die Seite, dass
Sie noch nichts freigeben dürfen.

### Datensätze anhaken {#tick-the-records}

Haken Sie sie unter **Einträge** an. **Wer hat Zugriff** neben einem Datensatz öffnet unter dem
Formular sein Feld **Wer was darf**, in dem Sie einzelne Berechtigungen ändern oder entziehen;
**Bearbeiten** öffnet den Datensatz.

### Wer und was festlegen {#choose-who-and-what}

Wählen Sie den Empfänger unter **Geben an** oder unter **Nicht aufgeführte Person**, haken Sie an,
was er **Darf**, und klicken Sie auf **Für Auswahl erteilen**.

{{% /steps %}}

Speichern ersetzt, was dieser Empfänger auf jedem angehakten Datensatz hat, ganz oder gar nicht:
Dürfen Sie es auf einem Datensatz nicht vergeben, wird nichts gespeichert, und der Fehler nennt die
ID dieses Datensatzes. Eine Gruppe muss existieren. Ein Benutzer, der sich noch nicht angemeldet
hat, wird angenommen, und der Hinweis sagt, dass der Zugriff gilt, sobald er sich anmeldet. `own`
wird hier nicht angeboten; Eigentum geben Sie an einem einzelnen Datensatz weiter.

## Einen Benutzer deaktivieren {#disable-a-user}

**/admin/users** (Administratoren) listet alle, die sich angemeldet haben, mit den Rollen und
Gruppen ihrer letzten Anmeldung. **Deaktivieren** beendet die Sitzungen des Benutzers sofort,
verweigert seine nächste Anmeldung und legt seine Webhook-Token still. **Aktivieren** macht das
rückgängig. Niemand kann sich selbst deaktivieren, und die lokale Anmeldung lässt sich nicht
deaktivieren.

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/users/disabled \
  -d '{"subject": "<subject>"}'
```

`DELETE` auf denselben Pfad aktiviert den Benutzer wieder. `GET /api/users?q=<text>` durchsucht die
Liste.

## Eine Rolle auf bestimmte Teams und Kanäle beschränken {#limit-a-role-to-some-teams-and-channels}

{{% steps %}}

### Berechtigungen öffnen {#open-permissions}

Öffnen Sie als Administrator **Berechtigungen** (**/admin/permissions**) und wählen Sie eine Rolle.

### Ankreuzen, was sie erreichen darf {#tick-what-it-may-reach}

Kreuzen Sie im Baum Teams und Kanäle an. Ein angekreuztes Team berechtigt alle seine Kanäle, auch
später hinzugefügte. Einzeln angekreuzte Kanäle berechtigen nur diese.

### Speichern {#save}

Speichern ersetzt in einer Transaktion, was die Rolle bisher hatte.

{{% /steps %}}

{{< callout type="info" >}}
Eine Rolle ohne Berechtigung erreicht alles. Die Einschränkung beginnt mit ihrer ersten
Berechtigung. Administratoren werden nie eingeschränkt.
{{< /callout >}}

Berechtigungen bestimmen, was eine Sitzung **sehen** darf, und ebenso, wohin sie zustellen darf.
Die Auswahllisten für Team und Kanal bieten nur Berechtigtes an, ein Ziel außerhalb der
Berechtigungen fehlt in den Listen und in der API, und ein Schreibzugriff oder eine Route, die
darüber hinaus zeigt, wird mit `403` abgelehnt.

Skripte verwenden `PUT /api/grants/role`, das die Berechtigungen einer Rolle wie die Seite ersetzt:

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/grants/role \
  -d '{"role": "editor", "scopes": [{"team_id": "<team id>"},
       {"team_id": "<team id>", "channel_id": "<channel id>"}]}'
```

Eine leere `channel_id` steht für das ganze Team. `/api/grants` fügt einzelne Berechtigungen hinzu
und listet sie.
