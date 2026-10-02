---
title: Container
weight: 2
---

Betreiben Sie Teamster aus dem veröffentlichten Container-Image, mit der Konfiguration aus einer
eingebundenen Datei und der Datenbank auf einem Volume.

## Das Image {#the-image}

Images werden unter `ghcr.io/pflege-de-labs/teamster` für `linux/amd64` und `linux/arm64`
veröffentlicht.

| Tag | Zeigt auf |
| --- | --- |
| `latest` | das neueste stabile Release |
| `X.Y.Z`, `X.Y` | dieses Release, zum Beispiel `0.11.0` und `0.11` |
| `<short-sha>` | den Build dieses Commits; bewegt sich nie |
| `main` | den neuesten Build von `main` |
| `pr-<n>` | den neuesten Build dieses Pull Requests |

Die Release-Tags eines Releases teilen sich einen Digest, und das enthaltene Binary meldet die
Release-Version. Ein Image von `main` oder aus einem Pull Request meldet stattdessen
`git describe`, etwa `v0.10.0-3-gabc1234`: das letzte Release, die Zahl der Commits danach und den
Commit. Deployen Sie ein `<short-sha>`-Tag, wenn Sie einen exakten Build brauchen, der sich nicht
bewegt.

Wie Sie vor dem Deployment eine Signatur, ein SBOM oder die Provenienz prüfen, beschreibt
[Release verifizieren](../../guides/verifying-a-release/).

## Starten {#run-it}

Schreiben Sie eine `config.yaml` wie im [Schnellstart]({{< ref "/docs/getting-started/quick-start" >}})
und binden Sie sie unter `/etc/xdg/teamster/config.yaml` ein, dem systemweiten Ort, den das Binary
durchsucht:

```bash
docker run --rm -p 8080:8080 --read-only \
  -e TEAMSTER_GRAPH_CLIENT_SECRET -e TEAMSTER_BOT_CLIENT_SECRET \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  ghcr.io/pflege-de-labs/teamster:<version>
```

* `/data` ist der einzige Pfad, in den der Dienst schreibt, daher funktioniert `--read-only`.
  `TEAMSTER_DATABASE_PATH` legt die SQLite-Datenbank bereits auf `/data/teamster.db`.
* Einstellungen können statt aus der Datei aus `TEAMSTER_*`-Umgebungsvariablen kommen. Ein Wert in
  der eingebundenen Datei hat Vorrang vor der Variable; halten Sie Secrets deshalb aus der Datei
  heraus.
* Das Image läuft als UID `65532` und hat weder Shell noch Paketmanager. Per `exec` gibt es darin
  nichts zu tun: Diagnostizieren Sie über die Container-Logs.

Setzen Sie `server.external-url` (`TEAMSTER_SERVER_EXTERNAL_URL`) auf die Adresse, unter der die
Verwaltungsoberfläche erreichbar ist, zum Beispiel `https://teamster.example.com`. Nachrichten, die
ohne Vorlage gesendet werden, verlinken dorthin. Der Wert muss eine absolute `http`- oder
`https`-URL sein.

## Health-Probes {#health-probes}

| Pfad | Antwortet, wenn |
| --- | --- |
| `GET /healthz` | der Prozess läuft |
| `GET /readyz` | er bedienen kann: Er fährt nicht herunter, und die Datenbank antwortet |

Beide sind ohne Authentifizierung erreichbar und beantworten `HEAD` ebenso wie `GET`.

* Liveness ignoriert die Datenbank. Ein Neustart des Prozesses bringt keine Datenbank zurück; er
  macht aus einem Ausfall nur eine Crash-Schleife.
* Readiness ignoriert Microsoft Graph. Die Instanz bei unerreichbarem Graph aus der Rotation zu
  nehmen, würde die Verwaltungsoberfläche genau dann schließen, wenn Sie sehen wollen, warum die
  Zustellung fehlschlägt.
* Readiness schlägt fehl, sobald das Herunterfahren beginnt. So schickt ein Rolling Update keinen
  Traffic mehr, bevor der Prozess keinen mehr annimmt.

Beim Herunterfahren lässt der Server laufende Anfragen bis zu `server.shutdown-timeout` (Standard
`15s`) auslaufen. Ein zweites Signal beendet ihn sofort.

## Nächste Schritte {#next-steps}

* [Teamster konfigurieren](../../guides/configuration/) für den produktiven Betrieb.
* [Speicher wählen und betreiben](../../guides/storage/), wenn Sie mehr als eine Instanz
  brauchen.
* [Auf Kubernetes installieren]({{< ref "/docs/getting-started/kubernetes" >}}) mit dem Helm-Chart.
