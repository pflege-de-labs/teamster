---
title: Schnellstart
weight: 1
---

In diesem Tutorial starten Sie Teamster auf Ihrem Rechner, richten eine Route auf einen Teams-Kanal
und senden ihr einen Alarm. Danach lösen Sie den Alarm auf und sehen zu, wie sich die Karte
ändert.

## Bevor Sie beginnen {#before-you-start}

Sie benötigen:

* Eine Entra-App-Registrierung für Microsoft Graph mit Tenant-ID, Client-ID und Client-Secret.
  Teamster listet damit Ihre Teams und Kanäle auf. Siehe
  [Microsoft-Graph-Berechtigungen erteilen](../../guides/graph-permissions/).
* Eine zweite Entra-Registrierung für den Teams-Bot mit Client-ID und Client-Secret. Der Bot
  postet jede Karte. Siehe [Den Teams-Bot einrichten](../../guides/teams-bot/).
* Die Teams-App von Teamster, installiert in dem Team, in das Sie posten möchten.
* `curl` sowie entweder Docker oder einen Linux- oder macOS-Rechner, auf dem das Binary läuft.

{{< callout type="info" >}}
Teamster startet auch ohne den Bot, aber dann erreicht keine Karte einen Kanal: Jede Zustellung
schlägt mit einem `502` fehl, der meldet, dass der Bot nicht konfiguriert ist.
{{< /callout >}}

{{% steps %}}

### Eine Konfigurationsdatei schreiben {#write-a-configuration-file}

Legen Sie in einem leeren Verzeichnis die Datei `config.yaml` an:

```yaml
admin:
  username: admin
  password: <admin-password>

graph:
  tenant-id: <tenant-id>
  client-id: <graph-client-id>

bot:
  tenant-id: <tenant-id>
  client-id: <bot-client-id>
```

Schreiben Sie die beiden Client-Secrets nicht in die Datei, sondern exportieren Sie sie. Teamster
liest eine Umgebungsvariable nur, wenn keine Konfigurationsdatei denselben Schlüssel setzt.

```bash
export TEAMSTER_GRAPH_CLIENT_SECRET='<graph-client-secret>'
export TEAMSTER_BOT_CLIENT_SECRET='<bot-client-secret>'
```

Alles andere behält seinen Standardwert: Der Server lauscht auf `:8080` und speichert seinen
Zustand in einer SQLite-Datei. [Teamster konfigurieren](../../guides/configuration/) erklärt, woher
die Konfiguration sonst noch kommen kann.

### Den Server starten {#start-the-server}

{{< tabs >}}
{{< tab name="Binary" >}}

Laden Sie das Release-Binary für Ihre Plattform herunter (`linux-amd64`, `linux-arm64`,
`darwin-amd64` oder `darwin-arm64`) und starten Sie es mit der Konfigurationsdatei:

```bash
curl -fLo teamster \
  https://github.com/pflege-de-labs/teamster/releases/latest/download/teamster-linux-amd64
chmod +x teamster
./teamster -c config.yaml
```

Die Datenbank wird als `teamster.db` in das aktuelle Verzeichnis geschrieben.

{{< /tab >}}
{{< tab name="Container" >}}

Binden Sie die Konfiguration an dem systemweiten Ort ein, den Teamster durchsucht, und legen Sie
die Datenbank auf ein Volume:

```bash
docker run --rm -p 8080:8080 \
  -e TEAMSTER_GRAPH_CLIENT_SECRET -e TEAMSTER_BOT_CLIENT_SECRET \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  ghcr.io/pflege-de-labs/teamster:latest
```

{{< /tab >}}
{{< /tabs >}}

`serve` ist der Standardbefehl und läuft, wenn Sie keinen angeben. Mit `Ctrl+C` beenden Sie den
Server.

### Anmelden {#sign-in}

Öffnen Sie <http://localhost:8080/admin> und melden Sie sich mit dem Admin-Benutzernamen und
-Passwort aus `config.yaml` an.

### Ein Ziel anlegen {#add-a-destination}

Geben Sie dem Ziel im Bereich **Ziele** einen Namen, wählen Sie **Team** und **Kanal** und
speichern Sie es.

Das erste Ziel, das Sie anlegen, wird zum **globalen Standard**: Alles, was keine Route
beansprucht, geht dorthin.

### Eine Route anlegen {#add-a-route}

Legen Sie im Bereich **Routen** eine Route an:

| Feld | Wert |
| --- | --- |
| Name | `Worker alerts` |
| Label-Selektor (JSON) | `{"service": "worker"}` |
| Liefert an | das Ziel, das Sie gerade angelegt haben |
| Vorlage | `Universal webhook (default)` |

Lassen Sie **Verfeinert (optional)** leer, damit dies eine Wurzelroute ist.

### Ein Token für den Absender ausstellen {#issue-a-token-for-the-sender}

Öffnen Sie <http://localhost:8080/admin/tokens>. Erstellen Sie ein Token mit dem Namen
`quick-start`, das an den universellen Webhook senden darf, und kopieren Sie es. Es wird nur
einmal angezeigt.

```bash
export TOKEN='<token>'
```

### Einen Alarm senden {#send-an-alert}

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8080/webhook/universal --data '{
    "key": "quick-start-1",
    "state": "open",
    "labels": {"alertname": "HighMemory", "severity": "warning", "service": "worker"},
    "attributes": {
      "summary": "Memory usage is above 80%",
      "description": "worker service memory is high"
    }
  }'
```

Teamster antwortet mit `{"status":"ok"}`, und im Kanal erscheint eine Karte für `HighMemory`. Das
Label `service=worker` hat auf Ihre Route gepasst.

### Den Alarm auflösen {#resolve-the-alert}

Senden Sie denselben `key` noch einmal, diesmal mit `state` auf `closed`:

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8080/webhook/universal --data '{
    "key": "quick-start-1",
    "state": "closed",
    "labels": {"alertname": "HighMemory", "severity": "warning", "service": "worker"},
    "attributes": {"summary": "Memory usage is back to normal"}
  }'
```

Die vorhandene Karte wechselt in den aufgelösten Zustand. Teamster postet keine zweite Karte, weil
es sich merkt, welche Karte zu dem Key gehört.

{{% /steps %}}

## Nächste Schritte {#next-steps}

* Prometheus anbinden: [Alarme aus Alertmanager senden](../../guides/alertmanager/).
* Eigene Karten gestalten: [Vorlagen schreiben](../../guides/templates/).
* Verstehen, welche Route ein Alarm nimmt: [Routing]({{< ref "/docs/concepts/routing" >}}).
* Produktiv betreiben: [Container]({{< ref "/docs/getting-started/container" >}}) oder
  [Kubernetes]({{< ref "/docs/getting-started/kubernetes" >}}).
