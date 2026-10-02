---
title: Ereignisse über den universellen Webhook senden
weight: 11
---

Den universellen Webhook verwenden Sie für jeden Absender, der nicht Alertmanager ist: einen
CI-Job, ein Cron-Skript, ein Monitoring-Werkzeug mit generischem Webhook. Der Absender schickt
Labels, nach denen geroutet wird, und Attribute, die gerendert werden. Den Rest erledigt Teamster.

Der Endpunkt ist `POST /webhook/universal`, authentifiziert mit `Authorization: Bearer <token>`.
Stellen Sie unter **/admin/tokens** ein Token aus und lassen Sie unter **Darf senden an** nur
`/webhook/universal` angehakt; siehe
[Einen Webhook-Absender authentifizieren](../webhook-tokens/).

## Eine einmalige Nachricht senden {#send-a-one-off-message}

Eine Nachricht ohne `state` wird einmal zugestellt und nie verfolgt. Sie braucht nur `labels` und
`attributes`:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  -d '{"labels": {"app": "checkout", "environment": "production"},
       "attributes": {"summary": "Deployment finished"}}' \
  http://localhost:8080/webhook/universal
```

Routen wählen nach `labels` aus. Vorlagen lesen `attributes` als `.Event.Universal.Attributes`.

{{< callout type="warning" >}}
Wiederholt der Absender eine Nachricht ohne `state` nach einem `502`, wird sie erneut an jedes Ziel
zugestellt, denn nichts kennzeichnet sie als dieselbe Nachricht. Verwenden Sie `state` und `key`,
wenn ein Duplikat stört.
{{< /callout >}}

## Ein Ereignis verfolgen, das sich öffnet und schließt {#track-an-event-that-opens-and-closes}

Mit `state` erhält ein Ereignis denselben Lebenszyklus wie ein Alertmanager-Alarm:

1. Senden Sie `"state": "open"` mit einem stabilen `key`. Teamster postet eine Karte.
2. Senden Sie erneut denselben `key` mit `"state": "open"`. Teamster bearbeitet die Karte an Ort
   und Stelle.
3. Senden Sie denselben `key` mit `"state": "closed"`. Teamster schließt die Karte.

```json
{
  "key": "checkout-latency",
  "state": "open",
  "labels": {"alertname": "HighLatency", "severity": "critical"},
  "attributes": {"summary": "p99 latency above 2s"},
  "time": "2025-12-07T20:07:00Z",
  "url": "https://grafana.example/d/checkout"
}
```

Ohne `key` leitet Teamster einen aus dem Webhook, `url`, `time` und den sortierten Labels ab. Jeder
`state` außer `open`, `closed` oder keinem wird mit `400` abgewiesen. Was in jedem Zustand mit
einer Karte geschieht, beschreibt [Lebenszyklus eines Alarms](../../concepts/alert-lifecycle/).

## Den Nachrichteninhalt direkt senden {#send-the-message-content-directly}

Ein Absender, der schon weiß, was er sagen will, kommt ohne Vorlage aus. Er fügt eines dieser
Felder hinzu:

| Feld | Gesendet als |
| --- | --- |
| `title` | Vorschauzeile im Aktivitätsfeed |
| `text` | Nachrichtentext, Markdown, bereinigt wie der einer Vorlage |
| `card` | Ein Adaptive-Card-JSON-Objekt |

Sie werden nur verwendet, wenn die zutreffende Route keine Vorlage hat. Eine Route mit Vorlage
rendert über diese und ignoriert die drei Felder. Eine Nachricht ohne Vorlage und ohne eines der
drei Felder geht trotzdem hinaus, mit der eingebauten Nachricht von Teamster: einem Titel aus
`summary` oder `alertname`, dem Zustand und der Beschreibung sowie dem Ereignis als JSON-Block.

Auf jede Nachricht ohne Vorlage folgt eine kleine Karte, die darauf hinweist. Setzen Sie
`server.external-url` (`TEAMSTER_SERVER_EXTERNAL_URL`) auf die Adresse der
Verwaltungsoberfläche, dann verlinkt diese Karte auf die Stelle, an der Vorlagen angelegt werden.

## Die Antwort prüfen {#check-the-answer}

| Status | Bedeutung |
| --- | --- |
| `200 {"status":"ok"}` | Zugestellt. |
| `200 {"status":"partial", …}` | An einige Personen zugestellt, nicht an alle. Siehe [Nachrichten an einzelne Personen senden](../direct-messages/). |
| `400` | Ungültiges JSON, ein unbekannter `state` oder mehr Empfänger als `webhook.max-recipients`. Korrigieren Sie die Anfrage. |
| `401`, `403` | Das Token wurde abgewiesen. Siehe [Wenn ein Absender abgewiesen wird](../webhook-tokens/#when-a-sender-is-refused). |
| `422` | Die Nachricht war an Personen gerichtet, und keine war erreichbar. |
| `502` | Etwas, das sich wieder erholen kann, ist ausgefallen: Teams, Graph, die Datenbank. Wiederholen Sie die Anfrage. |
| `503` | Das Token ließ sich nicht prüfen. Wiederholen Sie die Anfrage. |

Der Body einer `5xx`-Antwort enthält eine `request_id`. Suchen Sie im Serverlog danach, um die
Ursache zu finden.

## Beispiele {#samples}

Das Repository enthält ein Beispiel für jede Form:
[`universal-message.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-message.json),
[`universal-open.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-open.json),
[`universal-closed.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-closed.json)
und
[`universal-direct-message.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-direct-message.json).
Alle Felder führt [Webhook-Nutzdaten](../../reference/webhook-payloads/) auf.

## Einen Absender aus der Zeit vor 0.9.0 umstellen {#migrate-a-sender-written-before-090}

Release 0.9.0 hat die Felder der Nutzdaten umbenannt. Die alten Namen gibt es nicht mehr, auch
nicht als Alias. Benennen Sie sie um:

| Alt | Neu |
| --- | --- |
| `status: firing` | `state: open` |
| `status: resolved` | `state: closed` |
| `fingerprint` | `key` |
| `annotations` | `attributes` |
| `starts_at` | `time` |
| `generator` | `url` |
| `ends_at` | entfällt |

Siehe [ADR 0056](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0056-events-not-alerts.md).
