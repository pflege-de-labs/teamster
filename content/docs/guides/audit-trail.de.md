---
title: Änderungsprotokoll führen
weight: 19
---

Halten Sie fest, wer was an der Konfiguration von Teamster geändert hat, lesen Sie es in der
Verwaltungsoberfläche, und exportieren Sie es in eine Datei oder nach NATS JetStream für Ihr SIEM.

Das Änderungsprotokoll ist aus, bis Sie es konfigurieren. Ist es an, wird jede Änderung
aufgezeichnet: Vorlagen, Ziele, Routen, Webhook-Endpunkte, Zugriffstoken, Berechtigungen, Gruppen,
die Standardvorlage und das Standardziel, verknüpfte Chats und angeforderte Installationsläufe. Ein
Ereignis nennt, wer die Änderung vorgenommen hat, wie sich die Person angemeldet hat (`session`,
`basic` oder `cli`), die Request-ID und den Datensatz vorher und nachher. Zugangsdaten sind nie
enthalten.

## Protokoll in der Datenbank führen {#keep-the-trail-in-the-database}

```yaml
audit:
  database: true
```

Oder `TEAMSTER_AUDIT_DATABASE=true`. Administratoren lesen das Protokoll dann unter
**/admin/audit**, das neueste Ereignis zuerst, gefiltert nach **Wer**, **Aktion**,
**Ressourcentyp**, **Ressourcen-ID** und Zeit.

Skripte lesen dasselbe Protokoll mit `GET /api/audit`:

```bash
curl -u <admin-user>:<admin-password> \
  'http://localhost:8080/api/audit?type=Route&since=2026-10-01T00:00:00Z&limit=100'
```

| Parameter | Filtert nach |
| --- | --- |
| `actor`, `action`, `type`, `id` | Wer, was getan wurde, Ressourcentyp und -ID |
| `since`, `until` | Zeit, RFC 3339 |
| `limit` | Seitengröße, Standard 50 |
| `cursor`, `at` | Die nächste Seite: Geben Sie das Objekt `next` der vorigen Antwort zurück |

### Begrenzen, wie viel aufbewahrt wird {#bound-how-much-is-kept}

Die gezeigten Werte sind die Standards:

```yaml
audit:
  retention-age: "2160h"    # Ereignisse nach 90 Tagen vergessen; 0 behält sie unabhängig vom Alter
  retention-count: 100000   # höchstens so viele Ereignisse behalten; 0 ist unbegrenzt
  prune-interval: "1h"
```

Beide Grenzen gelten; maßgeblich ist die, die zuerst erreicht wird.

## Ereignisse zusätzlich in eine Datei schreiben {#also-write-events-to-a-file}

```yaml
audit:
  file: "/data/audit.jsonl"   # "-" ist stdout
```

Jedes Ereignis ist eine JSON-Zeile. Auf Kubernetes übergibt `-` die Ereignisse an Ihre
Log-Pipeline.

## Ereignisse nach NATS JetStream veröffentlichen {#publish-events-to-nats-jetstream}

{{% steps %}}

### Server festlegen {#set-the-server}

```yaml
audit:
  database: true
  nats:
    url: "nats://nats:4222"
    subject-prefix: "teamster.audit"
    stream: "TEAMSTER_AUDIT"
    create-stream: false      # true legt den Stream beim Start an oder aktualisiert ihn
    creds-file: ""            # JWT und NKey-Seed, falls der Server sie verlangt
```

Eine URL mit Zugangsdaten gehört in `TEAMSTER_AUDIT_NATS_URL`, nicht in die Datei.

### Stream anlegen {#create-the-stream}

Setzen Sie entweder `create-stream: true` und lassen Teamster den Stream anlegen, oder deklarieren
Sie ihn selbst, zum Beispiel [mit NACK](#declare-the-stream-with-nack). Der Stream muss
`<subject-prefix>.>` erfassen.

### Abonnieren {#subscribe}

Jedes Ereignis wird unter `<subject-prefix>.<resource type>.<action>` veröffentlicht, zum Beispiel
`teamster.audit.Route.route.delete`. Abonnieren Sie `teamster.audit.>` für alles oder
`teamster.audit.*.route.>` für Änderungen an Routen. Die Ereignis-ID ist die `Nats-Msg-Id`, daher
verwirft der Stream ein wiederholt gesendetes Duplikat.

{{% /steps %}}

Ein NATS-Server, der nicht läuft, hält Teamster nicht auf. Ereignisse warten je Senke in einer
Warteschlange der Größe `audit.queue-size` (Standard `1024`) und werden verworfen und gezählt, wenn
sie voll ist. `audit.nats.timeout` (Standard `5s`) begrenzt Verbindungsaufbau und Anlegen des
Streams.

Eine Senke erhält nur die Ereignisse, die aufgezeichnet werden, während sie konfiguriert ist. Wer
sie später einschaltet, bekommt keine älteren Ereignisse nachgeliefert.

### NATS-Ausfälle mit Backfill überbrücken {#ride-out-nats-outages-with-backfill}

Mit Backfill ist das Protokoll in der Datenbank der Puffer: Ereignisse werden daraus der Reihe nach
veröffentlicht, und die Position steht in der Datenbank. Teamster holt auf, sobald NATS wieder da
ist, auch nach einem Neustart.

```yaml
audit:
  database: true            # Pflicht
  nats:
    url: "nats://nats:4222"
    backfill: true
    backfill-interval: "2s" # wie oft nach neuen Ereignissen gesucht wird
    backfill-settle: "5s"   # wie alt ein Ereignis sein muss, bevor es veröffentlicht wird
```

* Backfill beginnt beim Einschalten mit dem neuesten Ereignis. Die Historie wird nicht gesendet.
* Änderungen durch `teamster import` veröffentlicht der laufende Server.
* Ein Ausfall, der länger dauert als die Aufbewahrung, verliert, was die Aufbewahrung inzwischen
  gelöscht hat. Bemessen Sie `retention-age` und `retention-count` für den längsten Ausfall, den Sie
  überstehen wollen.

Siehe [ADR 0071](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0071-publish-audit-events-to-nats-jetstream.md)
und [ADR 0078](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0078-the-trail-buffers-the-nats-export.md).

## Stream mit NACK deklarieren {#declare-the-stream-with-nack}

Auf Kubernetes kann das Helm-Chart den Stream und dauerhafte Consumer als
[NACK](https://github.com/nats-io/nack)-Ressourcen deklarieren.

{{< callout type="warning" >}}
Installieren Sie zuerst NACK. Das Chart installiert weder dessen CRDs noch dessen
JetStream-Controller. Ohne die CRDs schlägt die Installation fehl; ohne den Controller werden die
Ressourcen nie abgeglichen.
{{< /callout >}}

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm install nack nats/nack --namespace nats \
  --set jetstream.enabled=true --set jetstream.nats.url=nats://nats.nats.svc:4222
```

Dann in den Values des Teamster-Release:

```yaml
config:
  settings:
    audit:
      database: true
      nats:
        url: nats://nats.nats.svc:4222
        backfill: true
nack:
  stream:
    enabled: true
    spec:
      replicas: 3
      maxAge: 8760h
  consumers:
    siem:
      deliverPolicy: all
      ackPolicy: explicit
```

* Der `Stream` übernimmt Namen und Subjects aus `audit.nats.stream` und `subject-prefix`.
  `nack.stream.spec` wird über die Standards gelegt: Dateispeicher, eine Replica, ein
  `duplicateWindow` von zwei Minuten und `preventDelete: true`, das die Historie behält, wenn das
  Release deinstalliert wird.
* Jeder Schlüssel unter `nack.consumers` wird ein dauerhafter `Consumer` dieses Streams.
* `nack.account.create` erzeugt einen `Account` aus `nack.account.spec`, und `nack.account.name`
  verweist auf einen bestehenden. NACK gleicht `Account`-Ressourcen nur mit
  `jetstream.controlLoop=true` ab.
* Das Chart lehnt `nack.stream.enabled` zusammen mit `audit.nats.create-stream` ab, weil NACK
  rückgängig machen würde, was Teamster einstellt.

Übergeben Sie Teamsters eigene NATS-Zugangsdaten in `credentials.extra.TEAMSTER_AUDIT_NATS_URL`,
oder hängen Sie eine Creds-Datei mit `volumes` und `volumeMounts` ein und nennen Sie sie in
`audit.nats.creds-file`. Alle Schlüssel des Charts stehen unter
[Helm-Values](../../reference/helm-values/).

## Verlorene Ereignisse überwachen {#watch-for-lost-events}

Ein fehlgeschlagener Eintrag ins Änderungsprotokoll lässt die Änderung nie scheitern. Er wird
protokolliert und in `teamster.audit.failed` gezählt, und Ereignisse, die aus einer vollen
Warteschlange verworfen werden, zählt `teamster.audit.dropped`, beide nach `sink`. Alarmieren Sie
auf beide; siehe [Teamster überwachen](../observability/).
