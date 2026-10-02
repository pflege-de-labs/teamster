---
title: Nachrichten an einzelne Personen senden
weight: 12
---

Senden Sie eine Nachricht an die Personen, die sie nennt, jeweils in deren eigenen Teams-Chat statt
in einen Kanal. Typisch sind ein Hinweis auf ein ablaufendes Passwort oder ein Ticket, das jemandem
zugewiesen wurde.

Dafür muss der Teams-Bot eingerichtet sein, denn nur der Bot kann in den Chat einer Person
schreiben. Siehe [Den Teams-Bot einrichten](../teams-bot/). Mit `bot.global-install` kann Teamster
den Bot für Personen installieren, die ihn noch nicht haben.

## Personen adressieren {#address-people}

{{% steps %}}

### Eine Route anlegen, die Personen adressiert {#create-a-route-that-addresses-people}

Legen Sie in der Verwaltungsoberfläche eine Route an oder bearbeiten Sie eine, und setzen Sie
**Liefert an** auf **In der Nachricht genannte Personen**. Geben Sie ihr einen Label-Selektor für
die Nachrichten, die sie übernehmen soll, zum Beispiel `{"kind": "password-expiry"}`, und eine
Vorlage.

Jede Person erhält ihre eigene, für sie gerenderte Nachricht. Die Vorlage kann sie also persönlich
ansprechen:

```gotemplate
Hello {{ .Recipient.GivenName }}, your password expires on {{ .Event.Universal.Attributes.expires }}.
```

`.Recipient` ist in [Vorlagendaten](../../reference/template-data/) beschrieben.

### Die Personen in der Nachricht nennen {#name-the-people-in-the-message}

Am [universellen Webhook](../universal-webhook/) führen Sie sie im Feld `recipients` auf oberster
Ebene auf: UPNs, E-Mail-Adressen oder Entra-Objekt-IDs.

```json
{
  "key": "password-expiry-2026-10-07",
  "labels": {"kind": "password-expiry"},
  "recipients": ["alice@example.com", "bob@example.com"],
  "attributes": {"expires": "2026-10-07", "reset_url": "https://passwords.example.com/reset"},
  "time": "2026-09-30T08:00:00Z"
}
```

Ein Absender ohne ein solches Feld, etwa [Alertmanager](../alertmanager/), setzt stattdessen das
Label `teamster_recipient`, mehrere Adressen durch Kommas getrennt. Hat eine Nachricht beides, gilt
die Liste `recipients`. Leerzeichen um Adressen werden entfernt, Duplikate ohne Rücksicht auf
Groß- und Kleinschreibung verworfen.

### Die Antwort lesen {#read-the-answer}

| Antwort | Bedeutung |
| --- | --- |
| `200 {"status":"ok"}` | Alle wurden erreicht. |
| `200 {"status":"partial", "delivered": n, "undelivered": [...]}` | Einige Personen sind nicht erreichbar, und ein erneuter Versuch ändert daran nichts. |
| `422 {"status":"undelivered", ...}` | Niemand war erreichbar. Alertmanager wiederholt eine `4xx`-Antwort nicht. |
| `400` | Die Nachricht nennt mehr als `webhook.max-recipients` Personen. Nichts wurde gesendet. |
| `502` | Etwas, das sich wieder erholen kann, ist ausgefallen. Wiederholen Sie die Anfrage. |

Jeder Eintrag in `undelivered` enthält den `recipient` wie übergeben und einen `reason`:

| Grund | Bedeutung |
| --- | --- |
| `invalid-address` | Weder Objekt-ID noch UPN noch E-Mail-Adresse. |
| `unknown-recipient` | Keine solche Person im Verzeichnis. |
| `ineligible` | Kein aktiviertes Mitglied des Mandanten: ein Gast, ein deaktiviertes Konto, jemand, der ausgeschieden ist. |
| `not-installed` | Die Teams-App des Bots ist für diese Person nicht installiert. |
| `no-recipient` | Die Route adressiert Personen, und die Nachricht hat keine genannt. |
| `blocked` | Die Person hat den Bot blockiert oder entfernt. |

{{% /steps %}}

## Die Nachrichten aktualisieren oder schließen {#update-or-close-the-messages}

Mit `"state": "open"` und einem `key` erhalten Sie den verfolgten Lebenszyklus:

* Ein wiederholtes `open` mit demselben `key` bearbeitet die Nachricht jeder Person an Ort und
  Stelle.
* Ein `closed` mit demselben `key` sendet jeder Person eine neue Nachricht, dass es sich erledigt
  hat. Die Empfänger müssen dabei nicht erneut genannt werden. Es ist eine neue Nachricht statt
  einer Bearbeitung, weil Teams bei einer Bearbeitung nicht benachrichtigt.

Eine Nachricht ohne `key` erhält einen, der aus ihren Labels, ihrer Zeit, ihrer URL und ihren
Empfängern abgeleitet wird. Eine Nachricht ohne `state` wird einmal zugestellt, und erneut an alle,
wenn der Absender sie wiederholt.

## Die Grenzen anpassen {#tune-the-limits}

| Schlüssel | Umgebungsvariable | Standard | Wirkung |
| --- | --- | --- | --- |
| `webhook.max-recipients` | `TEAMSTER_WEBHOOK_MAX_RECIPIENTS` | `100` | Höchstzahl der Personen, die eine Nachricht nennen darf. Darüber wird sie mit `400` abgewiesen. |
| `webhook.fanout-concurrency` | `TEAMSTER_WEBHOOK_FANOUT_CONCURRENCY` | `8` | An wie viele Personen eine Nachricht gleichzeitig gesendet wird. |
| `bot.directory-ttl` | `TEAMSTER_BOT_DIRECTORY_TTL` | `24h` | Wie lange einer nachgeschlagenen Person vertraut wird, bevor Graph erneut gefragt wird. |
| `bot.inline-install-budget` | `TEAMSTER_BOT_INLINE_INSTALL_BUDGET` | `5` | Mit `bot.global-install`: für wie viele Personen eine Nachricht die App installieren darf. Die übrigen warten auf den nächsten Installationslauf. |

```yaml
webhook:
  max-recipients: 100
  fanout-concurrency: 8
bot:
  directory-ttl: 24h
  inline-install-budget: 5
```

Die Verteilung einer Anfrage muss innerhalb von `server.write-timeout` abgeschlossen sein. Erhöhen
Sie diesen Wert, wenn Sie die Grenzen erhöhen.

Den Entwurf beschreibt
[ADR 0063](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0063-a-message-names-its-recipients.md).
