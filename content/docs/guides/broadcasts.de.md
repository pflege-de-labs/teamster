---
title: Eine Nachricht an alle senden
weight: 13
---

Senden Sie eine Nachricht an alle, die der Bot erreichen kann, jeweils in ihren eigenen Teams-Chat,
ohne sie aufzuzählen. Typisch dafür sind eine Büroschließung oder eine Mitteilung an das ganze
Unternehmen.

Sie brauchen einen eingerichteten Teams-Bot. Alle sind:

* jedes aktivierte Mitglied des Mandanten, für das die App installiert ist, aus dem Verzeichnis,
  das `bot.global-install` pflegt, und
* jede Person, die ihren Chat verknüpft hat und nicht in diesem Verzeichnis steht,

jeweils einmal. Ohne `bot.global-install` sind das nur die Personen, die einen Chat verknüpft
haben. Personen, die den Bot blockiert haben, werden trotzdem angeschrieben; schlägt das fehl,
zählen sie als nicht erreichbar.

## Eine Rundsendung senden {#send-a-broadcast}

{{% steps %}}

### Die Stufe für Rundsendungen erhalten {#get-the-level-everyone}

Eine Rundsendung erfordert die Nachrichtenstufe **beliebige, und Rundsendung an alle**
(`everyone`), die das Nennen beliebiger Personen einschließt. Administratoren haben sie. Ein
Administrator erteilt sie anderen auf **/admin/access** unter **Wer Personen anschreiben darf**;
siehe [Jemanden Personen anschreiben lassen](../roles/#let-someone-message-people).

### Ein Token ausstellen, das Rundsendungen senden darf {#issue-a-token-that-may-broadcast}

Stellen Sie unter **/admin/tokens** ein Token für den universellen Webhook aus und setzen Sie
**Darf Empfänger nennen** auf **beliebige, und Rundsendung**. In der API ist das
`"messages": "everyone"`:

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/tokens \
  -d '{"name": "office-notices", "scope": ["universal"], "messages": "everyone"}'
```

Wie jedes Token sendet es Rundsendungen nur, solange sein Ersteller die Stufe noch hat. Siehe
[Ein Token Personen nennen lassen](../webhook-tokens/#let-a-token-name-people).

### Eine Route anlegen, die Personen adressiert {#create-a-route-that-addresses-people}

Legen Sie eine Route an, die die Nachricht selektiert, etwa mit `{"kind": "office-notice"}`, oder
bearbeiten Sie eine solche, und setzen Sie **Liefert an** auf
**Jede in der Nachricht genannte Person**. Jede Person erhält ihre eigene, für sie gerenderte Kopie;
die Vorlage kann also `.Recipient` verwenden. Für eine Person im Verzeichnis enthält es ihr ganzes
Entra-Profil, für jemanden, der nur einen Chat verknüpft hat, nur `ID` und `DisplayName`. Siehe
[Vorlagendaten](../../reference/template-data/#recipient).

Eine Rundsendung wird geroutet wie jede Nachricht. Kanäle und verknüpfte Chats, die sie trifft,
erhalten sie noch während der Anfrage. Nur die Routen, die an
**Jede in der Nachricht genannte Person** zustellen, senden sie an alle.

### Senden {#send-it}

Posten Sie an den [universellen Webhook](../universal-webhook/) mit `"broadcast": true`:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  -d '{"labels": {"kind": "office-notice"}, "text": "The office is closed on Friday.", "broadcast": true}' \
  http://localhost:8080/webhook/universal
```

Dasselbe liegt im Repository als `samples/universal-broadcast.json`. Eine Rundsendung darf weder
`recipients` noch das Label `teamster_recipient` noch einen `state` tragen: Sie wird einmal
zugestellt und nie aktualisiert oder geschlossen.

### Die Antwort lesen {#read-the-answer}

| Antwort | Bedeutung |
| --- | --- |
| `202 {"status":"accepted", "broadcast": {...}, "status_url": "/webhook/broadcasts/<id>", "delivered": n}` | In der Warteschlange. `delivered` zählt die Kanäle und verknüpften Chats, die während der Anfrage erreicht wurden. |
| `400` | Die Nachricht nennt zusätzlich Personen oder setzt `state`. Nichts wurde gesendet. |
| `403` | Das Token darf keine Rundsendungen senden. `webhook.token` und Tokens von vor 0.11.0 dürfen es nie. Siehe [Wenn ein Absender abgewiesen wird](../webhook-tokens/#when-a-sender-is-refused). |
| `422` mit Grund `no-addressed-route` | Keine Route, die an Personen zustellt, trifft zu. Nichts wurde gesendet. |
| `502` | Etwas, das sich wieder einrenken kann, ist fehlgeschlagen. Erneut versuchen. |

{{% /steps %}}

## Den Fortschritt verfolgen {#follow-its-progress}

Fragen Sie die `status_url` mit einem Token desselben Erstellers ab:

```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/webhook/broadcasts/<id>
```

`state` ist `requested`, solange sie wartet, `running`, während sie gesendet wird, danach `done`
oder `failed`. `total`, `delivered`, `unreachable` (Bot blockiert oder entfernt) und `failed`
zählen Personen. Das Token einer anderen Person erhält `404`.

**Rundsendungen** in der Verwaltungsoberfläche (**/admin/broadcasts**) listet Ihre eigenen
Rundsendungen auf, für Administratoren die aller. Der Eintrag erscheint, wenn der Bot eingerichtet
ist und Sie Rundsendungen senden dürfen. `GET /api/broadcasts` liefert dieselbe Liste. Beendete
Rundsendungen werden 30 Tage aufbewahrt.

## Was Sie erwarten können {#know-what-to-expect}

* Jedes Replikat mit Bot sucht nach wartenden Rundsendungen und sendet jeweils eine, 25 Personen
  pro Schritt, `webhook.fanout-concurrency` gleichzeitig.
* Fällt ein Replikat aus, übernimmt ein anderes die Rundsendung innerhalb von etwa zwei Minuten,
  ab dem letzten Schritt, den es festgehalten hat. Bis zu 25 Personen erhalten sie dann womöglich
  zweimal.
* Jeder Versand wird zusammen mit den übrigen Aufrufen des Bots getaktet, eine große Rundsendung
  braucht also ihre Zeit; siehe [Aufrufe an Teams takten](../teams-bot/#pace-the-calls-to-teams).
* Einen Versand, den Teams drosselt (`429`) oder für den es nicht verfügbar ist (`503`), wiederholt
  Teamster nach seinem `Retry-After`. Jeder andere fehlgeschlagene Versand wird gezählt, nicht
  wiederholt. Ebenso einer, der eine Minute nach Beginn seines Schritts von 25 noch wartet.
* Die Empfänger werden beim Start der Rundsendung gelesen. Wer später hinzukommt, erhält sie nur,
  wenn ein Replikat die Rundsendung übernimmt und diese Person hinter der Stelle liest, bis zu der
  es gekommen war.

Den Entwurf beschreibt
[ADR 0083](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0083-broadcasts-run-in-the-background.md).
