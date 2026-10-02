---
title: Architektur
weight: 4
---

Teamster ist ein einzelnes Go-Binary mit eingebetteter Verwaltungsoberfläche. Zur Laufzeit braucht
es eine Datenbank und zwei Entra-App-Registrierungen, sonst nichts. Diese Seite benennt die Teile,
mit denen Sie im Betrieb zu tun haben. Die Sicht für Entwickler, mit Paketen und
Designentscheidungen, steht im
[Architekturdokument](https://github.com/pflege-de-labs/teamster/blob/main/docs/architecture.md)
auf GitHub.

## Die Bestandteile {#the-parts}

```text
 Alertmanager ───► /webhook/alertmanager ─┬─► routing ─► template ─┐
 any sender ─────► /webhook/universal ────┘                        ├─► Teams bot ─► channel / chat
 Power Automate ─► /teamsv2/… ───────── its own channel ─► template ┘        ▲
                                                                             │ /bot/messages
 browser ────────► /admin, /api                                        Microsoft Teams
                      │
                      ├─► database (SQLite or Postgres)
                      └─► Microsoft Graph: Teams, channels, people, app installs
```

| Teil | Aufgabe |
| --- | --- |
| Webhooks | nehmen Ereignisse von Absendern an, die ein Token vorweisen; ein Teams-V2-Endpunkt trägt sein Token in der URL und postet in den einen Kanal, den er nennt |
| Routing | wählt anhand der Labels des Ereignisses Zustellziele und eine Vorlage |
| Vorlagen | rendern Titel, Text und Adaptive Card |
| Teams-Bot | postet und bearbeitet jede Kanalkarte und Chatnachricht über das Bot Framework |
| Microsoft Graph | listet Teams und Kanäle für die Auswahllisten auf, sucht Personen, installiert die Teams-App |
| Datenbank | enthält die Konfiguration, die offenen Alarme, Token, die angemeldeten Benutzer und das Änderungsprotokoll |
| Verwaltungsoberfläche und API | verwalten Vorlagen, Ziele, Routen, Token und Zugriff |

## Zwei Entra-Registrierungen {#two-entra-registrations}

Teamster verwendet zwei getrennte Zugangsdaten; wer die einen rotiert, berührt die anderen nie:

* **Die Graph-Registrierung** (`graph.*`) liest. Graph erlaubt einer Anwendung nicht, in Kanäle zu
  posten, deshalb postet diese Registrierung nie.
* **Die Bot-Registrierung** (`bot.*`) postet. Jede Karte wird über sie versendet. Deshalb erreicht
  ohne den Bot nichts Teams, und deshalb muss die Teams-App in jedem Team installiert sein, in das
  eine Route postet.

Siehe [Microsoft-Graph-Berechtigungen erteilen](../../guides/graph-permissions/) und
[Den Teams-Bot einrichten](../../guides/teams-bot/).

## Zustand {#state}

Alles, was Teamster sich merkt, liegt in seiner Datenbank: die Konfiguration, welche Nachricht zu
welchem offenen Alarm gehört, die Webhook-Token und das Änderungsprotokoll.

* **SQLite** ist eine Datei und braucht nichts weiter, erlaubt aber genau eine Instanz.
* **Postgres** lässt mehrere Instanzen hinter einem Service eine Datenbank teilen. Es gibt keine
  Queue, keine Leader-Wahl und kein Sharding: Jede Instanz kann jede Anfrage bearbeiten.

Schemamigrationen laufen, wenn eine Instanz die Datenbank öffnet, sofern `database.migrate` nichts
anderes festlegt. Siehe [Speicher auswählen und betreiben](../../guides/storage/).

## Listener {#listeners}

| Listener | Standard | Bedient |
| --- | --- | --- |
| `server.addr` | `:8080` | Webhooks, `/bot/messages`, Verwaltungsoberfläche und API, `/healthz`, `/readyz` |
| `metrics.addr` | `127.0.0.1:9090` | den Prometheus-Exporter, wenn `metrics.enabled` eingeschaltet ist; ohne Authentifizierung |

Die Webhooks und `/bot/messages` authentifizieren selbst: mit einem Token oder mit Microsofts
Signatur auf einer Bot-Aktivität. Verwaltungsoberfläche und API verlangen eine Sitzung oder Basic
Auth. Sie können die Webhooks daher ins Internet stellen und `/admin` in einem privaten Netz
halten. Zum Metrik-Listener siehe [Teamster überwachen](../../guides/observability/).

## Verwandte Themen {#related}

* [Routing]({{< ref "/docs/concepts/routing" >}}) und
  [Lebenszyklus eines Alarms]({{< ref "/docs/concepts/alert-lifecycle" >}}) erklären die Mitte des
  Diagramms.
* Die [Konfigurationsreferenz](../../reference/configuration/) listet jeden Schlüssel.
