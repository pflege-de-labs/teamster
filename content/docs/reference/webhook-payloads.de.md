---
title: Webhook-Nutzdaten
weight: 4
---

Die Endpunkte, die Nachrichten empfangen: wie sie authentifizieren, welche Körper sie annehmen und
was sie antworten.

## Endpunkte {#endpoints}

| Methode und Pfad | Authentifizierung | Körper | Geroutet |
| --- | --- | --- | --- |
| `POST /webhook/alertmanager` | `Authorization: Bearer <token>` | [Alertmanager](#alertmanager) | ja |
| `POST /webhook/universal` | `Authorization: Bearer <token>` | [Universell](#universal) | ja |
| `POST /teamsv2/{team}/{channel}/{token}` | Das Token im Pfad | [Teams V2](#teams-v2) | nein, postet an das Ziel des Endpunkts |
| `POST /bot/messages` | Signiertes Bot-Framework-Token | Bot-Framework-Activity | — |
| `GET /healthz` | keine | — | — |
| `GET /readyz` | keine | — | — |

`/bot/messages` wird nur registriert, wenn der Bot konfiguriert ist. Microsoft ruft ihn auf, kein
Absender sollte es tun. Sein Körper ist auf 256 KiB begrenzt.

`/healthz` antwortet `200 ok`, solange der Prozess läuft. `/readyz` antwortet `200 ok` oder `503`
mit `shutting down` bzw. `database unreachable`. Beide akzeptieren `GET` und `HEAD`.

Jede Antwort trägt einen Header `X-Request-ID`. Dieselbe ID wird als `request_id` protokolliert.

## Authentifizierung {#authentication}

`/webhook/alertmanager` und `/webhook/universal` akzeptieren beide Arten von Token.

| Token | Herkunft | Bereich |
| --- | --- | --- |
| Ausgestelltes Zugriffstoken, `tst_` und 64 Hexadezimalziffern | `/admin/tokens` oder `POST /api/tokens` | Die Webhooks, die es nennt, und nur, solange sein Ersteller sie noch verwenden darf. Ohne Bereich, wenn es ausgestellt wurde, bevor es Bereiche gab. |
| `webhook.token` | Konfiguration, `TEAMSTER_WEBHOOK_TOKEN` | Beide Webhooks. |

| Header | Status |
| --- | --- |
| `Authorization: Bearer <token>` | Aktuell. Groß- und Kleinschreibung des Schemas spielt keine Rolle. |
| `X-Teamster-Token: <token>` | Veraltet. Wird nur gelesen, wenn kein Header `Authorization: Bearer` vorhanden ist. |

Basic Auth wird nicht akzeptiert. Ohne `webhook.token` und ohne ausgestelltes Token wird jede
Anfrage abgewiesen.

Ein Teams-V2-Endpunkt authentifiziert allein über das Token aus 64 Hexadezimalziffern in seiner
URL. Gespeichert wird davon nur ein Digest.

## Alertmanager {#alertmanager}

Die [Webhook-Nutzdaten von Alertmanager](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config),
Version 4. Jeder Eintrag von `alerts` wird zu einem Ereignis und der Reihe nach verarbeitet.
Gelesen werden diese Felder; alle anderen werden ignoriert.

| Feld | Wird zu |
| --- | --- |
| `receiver` | `.Event.Alertmanager.Receiver` |
| `groupKey` | `.Event.Alertmanager.GroupKey` |
| `groupLabels` | `.Event.Alertmanager.GroupLabels` |
| `commonLabels` | `.Event.Alertmanager.CommonLabels` |
| `commonAnnotations` | `.Event.Alertmanager.CommonAnnotations` |
| `externalURL` | `.Event.Alertmanager.ExternalURL` |
| `alerts[].status` | `.Event.State`: `firing` wird `open`, `resolved` wird `closed`, alles andere bleibt leer |
| `alerts[].labels` | `.Event.Labels`, worauf Routen selektieren |
| `alerts[].annotations` | `.Event.Alertmanager.Annotations` |
| `alerts[].startsAt` | `.Event.Alertmanager.StartsAt` |
| `alerts[].endsAt` | `.Event.Alertmanager.EndsAt` |
| `alerts[].generatorURL` | `.Event.Alertmanager.GeneratorURL` |
| `alerts[].fingerprint` | `.Event.Key` |

Der erste Alarm, der fehlschlägt, beendet die Anfrage mit einem Fehler; die Alarme danach werden
nicht verarbeitet.

## Universell {#universal}

Ein JSON-Objekt. Jedes Feld ist optional.

| Feld | Typ | Bedeutung |
| --- | --- | --- |
| `key` | String | Kennzeichnet das Ereignis über mehrere Posts hinweg. Wird abgeleitet, wenn er fehlt, siehe [Schlüssel](#keys). |
| `state` | String | `open`, `closed` oder nicht vorhanden. Alles andere wird mit `400` abgewiesen. |
| `labels` | Objekt aus Strings | Worauf Routen selektieren. |
| `attributes` | Objekt aus Strings | Freitext für Vorlagen. In die Ereignis-Stichproben gehen nur die Schlüssel ein. |
| `time` | String, RFC 3339 | Wann das Ereignis begann. |
| `url` | String | Woher das Ereignis stammt. |
| `recipients` | Array aus Strings | Personen, an die eine adressierte Route zustellt: UPNs, E-Mail-Adressen oder Entra-Objekt-IDs. |
| `title` | String | Wird unverändert gesendet, wenn die Route keine Vorlage hat. |
| `text` | String, Markdown | Wird bereinigt gesendet, wenn die Route keine Vorlage hat. |
| `card` | Objekt, Adaptive Card | Wird unverändert gesendet, wenn die Route keine Vorlage hat. |

| `state` | Zustellung |
| --- | --- |
| `open` | Wird verfolgt. Eine Wiederholung mit demselben `key` aktualisiert die gesendete Nachricht. |
| `closed` | Schließt, was ein `open` mit demselben `key` gesendet hat. Braucht keine Empfänger. |
| nicht vorhanden | Wird einmal zugestellt und nicht verfolgt. Ein erneuter Versuch stellt erneut zu. |

```json
{
  "key": "optional-stable-id",
  "state": "open",
  "labels": {"alertname": "HighCPU", "severity": "critical"},
  "attributes": {"summary": "CPU spiking"},
  "time": "2025-12-07T20:07:00Z",
  "url": "https://grafana.example/d/cpu",
  "recipients": ["alex.example@example.com"]
}
```

Nutzdaten aus der Zeit, bevor Ereignisse die Alarme ablösten, verwendeten `status`,
`fingerprint`, `annotations`, `starts_at`, `ends_at` und `generator`. Diese Felder werden nicht
mehr gelesen.

## Labels, die Teamster liest {#labels-teamster-reads}

| Label | Gesetzt von | Bedeutung |
| --- | --- | --- |
| `teamster_source` | Teamster | Der Webhook, an dem das Ereignis ankam: `alertmanager`, `universal` oder `teamsv2`. Ein vom Absender gesetzter Wert wird überschrieben. |
| `teamster_recipient` | Dem Absender | Personen, an die eine adressierte Route zustellt, durch Kommas getrennt. Wird nur verwendet, wenn die Nutzdaten kein `recipients` enthalten. |

Empfänger werden von Leerraum befreit und ohne Rücksicht auf Groß- und Kleinschreibung
dedupliziert. Eine Nachricht darf höchstens `webhook.max-recipients` Personen nennen.

## Schlüssel {#keys}

Hat ein Ereignis keinen Schlüssel, leitet Teamster einen als SHA-256 ab über:

| Quelle | Gehasht |
| --- | --- |
| Alertmanager | `alertmanager`, `generatorURL`, `startsAt`, die Labels des Alarms nach Schlüssel sortiert |
| Universell | `universal`, `url`, `time`, die Labels nach Schlüssel sortiert und, falls vorhanden, die Empfänger |

## Antworten {#responses}

Für `/webhook/alertmanager` und `/webhook/universal`. Fehlerkörper haben die Form
`{"error": "<message>"}`.

| Status | Körper | Bedeutung |
| --- | --- | --- |
| `200` | `{"status": "ok"}` | Jede Zustellung war erfolgreich. |
| `200` | `{"status": "partial", "delivered": <n>, "undelivered": [...]}` | Einige Personen sind nicht erreichbar, und ein erneuter Versuch ändert daran nichts. |
| `400` | `{"error": "invalid JSON"}` | Der Körper ist nicht das erwartete JSON, auch bei einer fehlerhaften Zeitangabe. |
| `400` | `{"error": "unknown state …"}` | Nur universell: `state` ist weder `open` noch `closed` noch fehlt es. |
| `400` | `{"error": "the message names more recipients than webhook.max-recipients allows: …"}` | Zu viele Empfänger. |
| `401` | leer, mit `WWW-Authenticate: Bearer realm="webhook"` | Kein Token oder ein Token, das Teamster nicht kennt. |
| `403` | `{"error": "the token's scope, or its creator, does not allow this webhook"}` | Ein Token mit Bereich für einen Webhook, den es nicht nennt oder den sein Ersteller nicht mehr verwenden darf. |
| `405` | leer | Kein `POST`. |
| `422` | `{"status": "undelivered", "delivered": 0, "undelivered": [...]}` | Niemand war erreichbar. |
| `502` | `{"error": "bad gateway", "request_id": "<id>"}` | Etwas, das sich wieder einrenken kann, ist fehlgeschlagen: keine Route, eine Vorlage, die Datenbank, Teams. Erneut versuchen. |
| `503` | `{"error": "cannot check the token right now"}` | Die Datenbank hat auf die Token-Abfrage nicht geantwortet. Erneut versuchen. |

Ein `502` wegen eines fehlenden Bots nennt diesen statt `bad gateway`.

Jeder Eintrag von `undelivered`:

| Feld | Enthält |
| --- | --- |
| `recipient` | Die Adresse so, wie der Absender sie angegeben hat. Leer bei `no-recipient`. |
| `reason` | Einer der Gründe unten. |

| Grund | Bedeutung |
| --- | --- |
| `invalid-address` | Die Adresse ist weder UPN noch E-Mail-Adresse noch Objekt-ID. |
| `unknown-recipient` | Niemand im Verzeichnis hat diese Adresse. |
| `ineligible` | Kein aktiviertes Mitglied des Mandanten: ein Gast, ein deaktiviertes Konto oder jemand, der ausgeschieden ist. |
| `not-installed` | Die Teams-App ist für diese Person nicht installiert. |
| `no-recipient` | Die Route stellt an Personen zu, und die Nachricht hat keine genannt. |
| `blocked` | Die Person hat den Bot blockiert oder entfernt. |

## Teams V2 {#teams-v2}

Die Körper, die ein Webhook von Microsoft Teams Workflows annimmt. Die Form wird am Körper
erkannt.

| Form | Erkannt an | Zugestellt als |
| --- | --- | --- |
| MessageCard | `"@type": "MessageCard"` oder einem von `themeColor`, `sections`, `potentialAction` | Eine Adaptive Card. Nur `OpenUri`-Aktionen bleiben erhalten. `themeColor` wird zu einem Container-Stil. |
| Nachricht mit Anhängen | `attachments` | Jeder Anhang vom Typ `application/vnd.microsoft.card.adaptive`, unverändert weitergeleitet. |
| Text | Einem nicht leeren `text` | Text, bereinigt. |

```json
{
  "type": "message",
  "attachments": [
    {
      "contentType": "application/vnd.microsoft.card.adaptive",
      "content": {"type": "AdaptiveCard", "version": "1.4", "body": []}
    }
  ]
}
```

| Pfadsegment | Regel |
| --- | --- |
| `{team}`, `{channel}` | Kleinbuchstaben, Ziffern und Bindestriche, 1 bis 64 Zeichen, kein Bindestrich am Anfang oder Ende. Groß- und Kleinschreibung wird beim Abgleich ignoriert. |
| `{token}` | Das Token des Endpunkts, einmal angezeigt, wenn er angelegt oder das Token erneuert wird. |

Antworten. Fehlerkörper haben die Form `{"error": "<message>"}`.

| Status | Bedeutung |
| --- | --- |
| `200` | Gepostet. Körper `{"status": "ok"}`. |
| `400` | Kein JSON, oder weder Text noch Adaptive Card. |
| `401` | `invalid token`. |
| `404` | `unknown endpoint`: Kein Endpunkt hat dieses Team und diesen Kanal, oder der Pfad ist unvollständig. |
| `405` | Kein `POST`. Trägt `Allow: POST`. |
| `413` | `payload too large`: Der Körper ist größer als 128 KiB. |
| `502` | Teams, die Datenbank oder die Vorlage des Endpunkts ist fehlgeschlagen. |

Nichts wird verfolgt. Eine Teams-V2-Nachricht wird nie aktualisiert oder geschlossen.

## Siehe auch {#see-also}

* [Alarme aus Alertmanager senden](../../guides/alertmanager/)
* [Ereignisse über den universellen Webhook senden](../../guides/universal-webhook/)
* [Einen Absender von einem Teams-Workflows-Webhook umziehen](../../guides/teams-v2-webhook/)
* [Einen Webhook-Absender authentifizieren](../../guides/webhook-tokens/)
* [Vorlagendaten](../template-data/)
