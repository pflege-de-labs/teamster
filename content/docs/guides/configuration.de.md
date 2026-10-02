---
title: Teamster konfigurieren
weight: 1
---

Teamster liest seine Einstellungen aus YAML-Dateien, Umgebungsvariablen und Kommandozeilenflags.
Diese Anleitung zeigt, woher jede Einstellung kommt und welche gewinnt. Alle Schlüssel mit ihrem
Standardwert und ihrer Umgebungsvariable stehen in der
[Konfigurationsreferenz](../../reference/configuration/); `teamster --help` gibt dieselbe Liste aus.

## Die Konfigurationsdatei dort ablegen, wo Teamster sie findet {#put-the-config-file-where-teamster-finds-it}

Teamster sucht diese Dateien in dieser Reihenfolge, nach der XDG Base Directory Specification:

1. `$XDG_CONFIG_DIRS/teamster/config.yaml` (Standard `/etc/xdg/teamster/config.yaml`)
2. `$XDG_CONFIG_HOME/teamster/config.yaml` (Standard `~/.config/teamster/config.yaml`)
3. `./config.yaml` im Arbeitsverzeichnis

Jede vorhandene Datei wird gelesen, und eine spätere überschreibt eine frühere Schlüssel für
Schlüssel. Nennt `$XDG_CONFIG_DIRS` mehrere Verzeichnisse, hat das zuerst genannte Vorrang, wie es
die Spezifikation vorsieht.

Um die Suche zu überspringen und nur eine Datei zu lesen, geben Sie sie ausdrücklich an:

```bash
teamster --config /path/to/config.yaml
```

`-c` ist die Kurzform, und `TEAMSTER_CONFIG` setzt dasselbe über die Umgebung.

Beginnen Sie mit
[`config.example.yaml`](https://github.com/pflege-de-labs/teamster/blob/main/config.example.yaml);
sie enthält jeden Schlüssel mit einem Kommentar.

## Schlüssel mit Bindestrichen schreiben {#write-the-keys-hyphenated}

YAML-Schlüssel sind die Flag-Namen, nach ihrem Präfix verschachtelt. Das Flag `--graph-tenant-id`
ist der Schlüssel `tenant-id` unter `graph`:

```yaml
graph:
  tenant-id: "<tenant-id>"
  client-id: "<client-id>"
  client-secret: "<client-secret>"
```

Unbekannte Schlüssel ignoriert Teamster. Ein falsch geschriebener Pflichtschlüssel fällt beim Start
als Validierungsfehler auf; ein falsch geschriebener optionaler Schlüssel behält stillschweigend
seinen Standardwert. Scheint eine Einstellung wirkungslos, prüfen Sie ihre Schreibweise anhand der
[Referenz](../../reference/configuration/).

## Geheimnisse in Umgebungsvariablen halten {#keep-secrets-in-environment-variables}

Jedes Flag liest auch eine Umgebungsvariable: `TEAMSTER_`, gefolgt vom Flag-Namen in
Großbuchstaben mit Unterstrichen. `graph.client-secret` ist `TEAMSTER_GRAPH_CLIENT_SECRET`,
`server.addr` ist `TEAMSTER_SERVER_ADDR`.

Die Rangfolge, höchste zuerst:

1. Kommandozeilenflags
2. Konfigurationsdateien
3. Umgebungsvariablen
4. eingebaute Standardwerte

{{< callout type="warning" >}}
Eine Umgebungsvariable gilt nur, wenn keine Konfigurationsdatei den Schlüssel setzt. Ein auf `""`
gesetzter Schlüssel gilt als gesetzt: Er überschreibt die Variable aus Ihrem Secret Store. Löschen
Sie den Schlüssel oder kommentieren Sie ihn aus. `config.example.yaml` setzt mehrere Geheimnisse
auf `""`; entfernen Sie diese Zeilen, wenn Sie die Datei kopieren.
{{< /callout >}}

Halten Sie diese Werte aus der Konfigurationsdatei heraus und übergeben Sie sie als
Umgebungsvariablen:

| Schlüssel | Umgebungsvariable |
| --- | --- |
| `webhook.token` | `TEAMSTER_WEBHOOK_TOKEN` |
| `admin.password` | `TEAMSTER_ADMIN_PASSWORD` |
| `graph.client-secret` | `TEAMSTER_GRAPH_CLIENT_SECRET` |
| `bot.client-secret` | `TEAMSTER_BOT_CLIENT_SECRET` |
| `auth.oidc-client-secret` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` |
| `auth.broker.token-encryption-key` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` |
| `database.postgres.password` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` |
| `database.postgres.url` | `TEAMSTER_DATABASE_POSTGRES_URL` |

Das Helm-Chart nimmt Ihnen diese Aufteilung ab: `config.settings` wird zur Konfigurationsdatei und
`credentials` zur Umgebung. Siehe die [Referenz der Helm-Values](../../reference/helm-values/).

Ohne `graph.tenant-id`, `graph.client-id`, `graph.client-secret`, `admin.username` und
`admin.password` startet Teamster nicht.

## Mit einem anderen als dem öffentlichen Graph sprechen {#talk-to-something-other-than-the-public-graph}

Drei Schlüssel legen fest, mit welchem Microsoft Graph Teamster spricht:

| Schlüssel | Standard |
| --- | --- |
| `graph.base-url` | `https://graph.microsoft.com/v1.0` |
| `graph.token-url` | leer: `https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token` |
| `graph.scope` | `https://graph.microsoft.com/.default` |

Eine normale Installation lässt alle drei unverändert. Für eine souveräne Cloud ändern Sie alle
drei gemeinsam. Um die Zustellung ohne Microsoft-Tenant zu testen, richten Sie sie auf einen Server
unter Ihrer Kontrolle:

```yaml
graph:
  base-url: "http://127.0.0.1:18500"
  token-url: "http://127.0.0.1:18500/token"
  scope: "http://127.0.0.1:18500/.default"
```

Der Bot hat dieselben Stellschrauben für das Bot Framework: `bot.token-url`, `bot.scope` und
`bot.metadata-url`. Auch diese ändert eine souveräne Cloud gemeinsam. `bot.metadata-url` muss eine
`https`-URL bleiben; siehe [Den Teams-Bot einrichten](../teams-bot/).

## Die Sprache der Verwaltungsoberfläche festlegen {#set-the-admin-ui-language}

Die Verwaltungsoberfläche gibt es auf Englisch und Deutsch. Der `Accept-Language`-Header des
Browsers wählt zwischen beiden. `ui.language` (`TEAMSTER_UI_LANGUAGE`, Standard `en`) gilt, wenn
der Browser keine der beiden nennt:

```yaml
ui:
  language: "de"
```

Jede Person kann das überschreiben: mit den Flaggen im Kontomenü oder mit der Auswahl im Kopf der
Anmeldeseite. Die Wahl wird in einem Cookie gespeichert. **Browsereinstellung** (die Weltkugel)
kehrt zu `Accept-Language` zurück.

Um Formulierungen zu ändern oder eine Sprache hinzuzufügen, ohne auf ein Release zu warten, richten
Sie `ui.locale-dir` auf ein Verzeichnis mit JSON-Dateien, die nach ihrer Sprache benannt sind, etwa
`de.json` oder `pt-BR.json`. Jeder Eintrag überschreibt einen eingebauten Text:

```json
{ "nav.routing": "Wegefindung" }
```

Ein Schlüssel, den kein Katalog enthält, erscheint als der Schlüssel selbst; ein fehlender Eintrag
fällt also auf. Webhook-Antworten, API-Fehler und Logzeilen sind immer auf Englisch.

## Zeiten in Ihrer eigenen Zeitzone anzeigen {#show-times-in-your-own-time-zone}

Die Verwaltungsoberfläche zeigt Zeiten in UTC, mit genannter Zone. Um sie in der Zeitzone Ihres
Browsers zu sehen, wählen Sie im Kontomenü **Browserzeit**; **UTC** schaltet zurück. Die Wahl wird
je Browser in einem Cookie gespeichert.

Die Browserzeit braucht JavaScript; ohne JavaScript bleiben die Zeiten in UTC. API-Antworten und
Logs sind immer in UTC. Eine serverseitige Einstellung dafür gibt es nicht.

## Shell-Vervollständigung installieren {#install-shell-completion}

Teamster erzeugt Vervollständigungsskripte für bash, zsh und fish:

```bash
teamster completion bash > /etc/bash_completion.d/teamster
teamster completion zsh  > "${fpath[1]}/_teamster"
teamster completion fish > ~/.config/fish/completions/teamster.fish
```

Das Skript wird aus dem Befehlsbaum erzeugt und kennt daher jeden Befehl, jedes Flag und die Werte
eines Enum-Flags: `--database-driver <TAB>` bietet `sqlite` und `postgres` an. Erzeugen Sie es nach
einem Upgrade neu, damit neue Flags dazukommen.

{{< callout type="info" >}}
In bash vervollständigt ein Enum-Flag zum aktuellen Wert seiner Umgebungsvariable statt zu seinen
erlaubten Werten: zu nichts, wenn die Variable nicht gesetzt ist. Befehle und Flags werden normal
vervollständigt, und zsh und fish bieten die echten Werte an.
{{< /callout >}}
