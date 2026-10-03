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

### Ein Token ausstellen, das Personen nennen darf {#issue-a-token-that-may-name-people}

Eine Nachricht, die jemanden nennt, wird mit `403` abgewiesen, wenn ihr Token diese Personen nicht
nennen darf. Setzen Sie unter **/admin/tokens** beim Ausstellen des Tokens für den Absender
**Darf Empfänger nennen**:

| Auswahl | API-Wert | Das Token darf nennen |
| --- | --- | --- |
| niemanden | fehlt | Niemanden. Jede Nachricht, die Personen nennt, wird abgewiesen. |
| nur mich | `self` | Nur Sie, den Ersteller des Tokens. |
| beliebige | `anyone` | Jede Person im Mandanten. |
| beliebige, und Rundsendung | `everyone` | Jede Person, und [alle auf einmal](../broadcasts/). |

**beliebige** wird nur angeboten, wenn ein Administrator Ihnen erlaubt hat, beliebige Personen
anzuschreiben; siehe [Jemanden Personen anschreiben lassen](../roles/#let-someone-message-people).
Ein Absender für ablaufende Passwörter braucht diese Stufe. **nur mich** passt zu einem Skript,
das seinen eigenen Autor benachrichtigt. Die Einzelheiten stehen unter
[Ein Token Personen nennen lassen](../webhook-tokens/#let-a-token-name-people).

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
Label `teamster_recipient`, mehrere Adressen durch Kommas getrennt. Bei einer Alertmanager-Gruppe
werden die Werte des Labels aus allen ihren Alarmen zusammengeführt. Hat eine Nachricht beides,
gilt die Liste `recipients`. Leerzeichen um Adressen werden entfernt, Duplikate ohne Rücksicht auf
Groß- und Kleinschreibung verworfen.

In einer Alerting-Regel von Alertmanager setzen Sie das Label aus einem Label, das der Alarm
bereits trägt, oder nennen eine feste Person:

```yaml
groups:
  - name: certificates
    rules:
      - alert: CertificateExpiring
        expr: cert_expiry_seconds < 7 * 86400
        labels:
          kind: certificate-expiry
          teamster_recipient: "{{ $labels.owner_email }}"
      - alert: BackupFailed
        expr: backup_last_success_age_seconds > 86400
        labels:
          kind: backup
          teamster_recipient: "alice@example.com,bob@example.com"
```

Jeder Alarm einer Gruppe wird über sein eigenes Label adressiert. Eine Benachrichtigung kann also
für verschiedene Alarme verschiedene Personen erreichen. Was eine Adresse sein darf, steht unter
[Wie Adressen zugeordnet werden](#how-addresses-are-matched).

### Die Antwort lesen {#read-the-answer}

| Antwort | Bedeutung |
| --- | --- |
| `200 {"status":"ok"}` | Alle wurden erreicht. |
| `200 {"status":"partial", "delivered": n, "undelivered": [...]}` | Einige Personen sind nicht erreichbar, und ein erneuter Versuch ändert daran nichts. |
| `422 {"status":"undelivered", ...}` | Niemand war erreichbar. Alertmanager wiederholt eine `4xx`-Antwort nicht. |
| `400` | Die Nachricht nennt mehr als `webhook.max-recipients` Personen. Nichts wurde gesendet. |
| `403` | Das Token darf diese Personen nicht nennen. Nichts wurde gesendet. Siehe [Wenn ein Absender abgewiesen wird](../webhook-tokens/#when-a-sender-is-refused). |
| `502` | Etwas, das sich wieder erholen kann, ist ausgefallen. Wiederholen Sie die Anfrage. |

Jeder Eintrag in `undelivered` enthält den `recipient` wie übergeben und einen `reason`:

| Grund | Bedeutung |
| --- | --- |
| `invalid-address` | Weder Objekt-ID noch UPN noch E-Mail-Adresse. |
| `unknown-recipient` | Keine solche Person im Verzeichnis. |
| `ambiguous-address` | Mehr als eine Person trägt diese E-Mail-Adresse. Verwenden Sie ihren UPN oder ihre Objekt-ID. |
| `ineligible` | Kein aktiviertes Mitglied des Mandanten: ein Gast, ein deaktiviertes Konto, jemand, der ausgeschieden ist. |
| `not-installed` | Die Teams-App des Bots ist für diese Person nicht installiert. |
| `no-recipient` | Die Route adressiert Personen, und die Nachricht hat keine genannt. |
| `blocked` | Die Person hat den Bot blockiert oder entfernt. |

{{% /steps %}}

## Wie Adressen zugeordnet werden {#how-addresses-are-matched}

Jede Adresse hat eine von drei Formen. Groß- und Kleinschreibung spielt bei der Zuordnung keine
Rolle.

| Form | Beispiel | Trifft |
| --- | --- | --- |
| Entra-Objekt-ID | `0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0` | Den Benutzer mit dieser ID. Nur eine GUID gilt als ID. |
| User Principal Name | `alice@example.com` | Den Benutzer, dessen UPN sie ist. |
| E-Mail-Adresse | `a.smith@example.com` | Den Benutzer, dessen primäre E-Mail-Adresse oder SMTP-Alias sie ist. Gilt, wenn kein UPN passt. |

Alles ohne `@`, das keine GUID ist, wird mit `invalid-address` beantwortet.

* **Wo Teamster sucht.** Zuerst im eigenen Personenverzeichnis, das ein früheres Nachschlagen
  oder der [Installationslauf](../teams-bot/#install-the-bot-for-everyone) gefüllt hat. Ein
  Eintrag, der älter als `bot.directory-ttl` (Standard `24h`) ist, wird erneut in Microsoft Graph
  nachgeschlagen. Aliase findet Teamster nur über Graph, die erste Nachricht an einen Alias kostet
  also einen Graph-Aufruf.
* **Ein Tippfehler wird gemerkt.** Eine Adresse, die Graph nicht kennt, wird 10 Minuten lang mit
  `unknown-recipient` beantwortet, ohne Graph erneut zu fragen. Eine gerade in Entra angelegte
  Person kann so lange brauchen, bis sie unter einer Adresse erreichbar ist, die zuvor
  fehlgeschlagen ist.
* **Wer erreicht wird.** Nur aktivierte Mitglieder des Mandanten. Gäste, deaktivierte Konten und
  ausgeschiedene Personen werden mit `ineligible` beantwortet.
* **Eine Adresse, die zwei Personen teilen.** Eine E-Mail-Adresse oder ein Alias, den mehr als ein
  Benutzer trägt, nennt niemanden eindeutig. Sie wird mit `ambiguous-address` beantwortet: Nennen
  Sie die Person stattdessen über UPN oder Objekt-ID.
* **nur mich.** Ein auf seinen Ersteller beschränktes Token darf jede der drei Formen verwenden,
  solange die Adresse auf die eigene Objekt-ID des Erstellers aufgelöst wird.

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
Sie diesen Wert, wenn Sie die Grenzen erhöhen. Jeder Versand wartet außerdem, bis die Taktung des
Bots ihn an die Reihe lässt, und ein gedrosselter wartet sein `Retry-After` ab. Ein Versand, der
über die Frist hinaus warten müsste, schlägt fehl; der Absender erhält ein `502` und wiederholt.
Siehe [Aufrufe an Teams takten](../teams-bot/#pace-the-calls-to-teams).

Den Entwurf beschreibt
[ADR 0063](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0063-a-message-names-its-recipients.md).
