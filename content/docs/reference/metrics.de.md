---
title: Metriken
weight: 5
---

Alle Metriken, die Teamster exportiert, mit Typ und Attributen. Metriken sind abgeschaltet, solange
`metrics.enabled` nicht gesetzt ist; siehe [Konfiguration](../configuration/#metrics).

## Namen {#names}

Die Instrumente tragen Namen nach OpenTelemetry-Konvention. Der Prometheus-Exporter ersetzt darin
Punkte durch Unterstriche und hängt Suffixe für Einheit und Typ an.

| OpenTelemetry | Prometheus |
| --- | --- |
| `teamster.deliveries` (Counter) | `teamster_deliveries_total` |
| `teamster.active_events` (Gauge) | `teamster_active_events` |
| `http.server.request.duration` (Histogramm, Sekunden) | `http_server_request_duration_seconds` |

Attributnamen behalten über OTLP ihre Punkte und verwenden in Prometheus Unterstriche (aus
`http.route` wird `http_route`).

## Teamster-Metriken {#teamster-metrics}

| Metrik | Prometheus-Name | Typ | Einheit | Attribute | Beschreibung |
| --- | --- | --- | --- | --- | --- |
| `teamster.deliveries` | `teamster_deliveries_total` | Counter | `{delivery}` | `route`, `outcome` | An einen Kanal oder eine Person zugestellte Nachrichten, nach Route und Ergebnis. |
| `teamster.webhook.receipts` | `teamster_webhook_receipts_total` | Counter | `{message}` | `source`, `state` | Auf einem Webhook empfangene Nachrichten, abgewiesene eingeschlossen. |
| `teamster.render.failures` | `teamster_render_failures_total` | Counter | `{failure}` | `template`, `stage` | Nachrichten, die sich nicht rendern ließen. |
| `teamster.active_events` | `teamster_active_events` | Gauge | `{card}` | — | Derzeit verfolgte Karten, eine je Ereignis und Kanal. |
| `teamster.destinations.without_app` | `teamster_destinations_without_app` | Gauge | `{destination}` | — | Ziele in einem Team, für das nicht bekannt ist, dass die Teams-App des Bots dort installiert ist. |
| `teamster.app.installs` | `teamster_app_installs_total` | Counter | `{install}` | `outcome` | Versuche, die Teams-App des Bots zu einer Person zu bringen. |
| `teamster.directory.lookups` | `teamster_directory_lookups_total` | Counter | `{lookup}` | `result` | Zu einer Person aufgelöste Adressen, nach Herkunft der Antwort. |
| `teamster.directory.runs` | `teamster_directory_runs_total` | Counter | `{run}` | `kind`, `outcome` | Läufe, die die Teams-App für den Mandanten installieren. |
| `teamster.directory.users` | `teamster_directory_users` | Gauge | `{person}` | `state` | Personen im Verzeichnis, nach Installationsstatus. |
| `teamster.audit.failed` | `teamster_audit_failed_total` | Counter | `{event}` | `sink` | Audit-Ereignisse, die ein Sink abgelehnt hat. |
| `teamster.audit.dropped` | `teamster_audit_dropped_total` | Counter | `{event}` | `sink` | Audit-Ereignisse, die verworfen wurden, weil die Warteschlange eines Sinks voll war. |
| `teamster.throttled` | `teamster_throttled_total` | Counter | `{call}` | `api` | Aufrufe, die Microsoft Graph oder der Bot Connector mit `429` oder `503` abgewiesen hat, nach API. Jeder abgewiesene Versuch zählt. |
| `teamster.pacing.wait` | `teamster_pacing_wait_seconds` | Histogramm | `s` | — | Zeit, die ein Bot-Connector-Aufruf auf das Budget des Bots gewartet hat. Siehe [Aufrufe an Teams takten](../../guides/teams-bot/#pace-the-calls-to-teams). |

Ein Counter erscheint in der Ausgabe, sobald er etwas gezählt hat. Die Gauges werden beim Abruf aus
der Datenbank gelesen, und jede Antwort wird eine Sekunde lang wiederverwendet.

## HTTP-Metriken {#http-metrics}

Erfasst von der OpenTelemetry-HTTP-Instrumentierung, mit deren Standardattributen.

| Metrik | Prometheus-Name | Typ | Beschreibung |
| --- | --- | --- | --- |
| `http.server.request.duration` | `http_server_request_duration_seconds` | Histogramm | Zeit bis zur Antwort auf eine Anfrage an `server.addr`, nach `http.route`, Methode und Status. `/healthz` und `/readyz` werden nicht erfasst. |
| `http.client.request.duration` | `http_client_request_duration_seconds` | Histogramm | Antwortzeit von Microsoft Graph und Bot Connector, nach Serveradresse, Methode und Status. |

Die Histogramme sind exponentiell und werden an Prometheus als native Histogramme exportiert.
Histogramme für die Größe von Anfrage- und Antwortkörpern werden verworfen.

## Attributwerte {#attribute-values}

| Metrik | Attribut | Werte |
| --- | --- | --- |
| `teamster.deliveries` | `route` | Name der Route oder, wenn sie keinen hat, ihre ID. Für Teams V2 `teamsv2:<team>/<channel>`. |
| `teamster.deliveries` | `outcome` | `posted`, `updated`, `failed`, `blocked`, `app_missing`; für eine nicht erreichbare Person `invalid-address`, `unknown-recipient`, `ineligible`, `not-installed`, `no-recipient` |
| `teamster.webhook.receipts` | `source` | `alertmanager`, `universal`, `teamsv2`, `bot` |
| `teamster.webhook.receipts` | `state` | `alertmanager`, `universal`: `open`, `closed`, leer ohne Status, `refused` (unbekanntes oder fehlendes Token), `forbidden` (Bereich des Tokens). `teamsv2`: `accepted`, `refused`, `unknown`, `rejected`, `error`. `bot`: `accepted` oder der Grund, aus dem die Nachricht abgewiesen oder ignoriert wurde. |
| `teamster.render.failures` | `template` | ID der Vorlage; leer, wenn keine gefunden wurde. |
| `teamster.render.failures` | `stage` | `template`, `destination`, `recipient`, `render` |
| `teamster.app.installs` | `outcome` | `installed`, `already`, `failed`, `ineligible` |
| `teamster.directory.lookups` | `result` | `store`, `graph`, `negative-cache`, `unknown` |
| `teamster.directory.runs` | `kind` | `manual`, `periodic` |
| `teamster.directory.runs` | `outcome` | `done`, `failed`, `lost` |
| `teamster.directory.users` | `state` | `unknown`, `installed`, `removed`, `failed`, `ineligible`, `departed` |
| `teamster.audit.failed`, `teamster.audit.dropped` | `sink` | `database`, `file`, `nats` |
| `teamster.throttled` | `api` | `bot`, `graph` |

`app_missing` ist ein Kanal-Post, den der Bot Connector abgelehnt hat, fast immer, weil die
Teams-App in diesem Team nicht installiert ist. Wie `blocked` bleibt der Zustand bestehen, bis
jemand handelt.

## Ressource {#resource}

| Attribut | Wert |
| --- | --- |
| `service.name` | `metrics.service-name`, Standard `teamster` |

In Prometheus erscheint die Ressource als `target_info`.

## Grenzen {#limits}

| Grenze | Wert |
| --- | --- |
| Zeitreihen je Metrik | 2000. Weitere Attributkombinationen werden in einer Überlauf-Zeitreihe zusammengefasst. |

## Siehe auch {#see-also}

* [Teamster überwachen](../../guides/observability/)
* [Konfiguration: Metriken](../configuration/#metrics)
* [Helm-Werte: Metriken](../helm-values/#metrics)
