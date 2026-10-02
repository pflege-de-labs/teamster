---
title: Teamster überwachen
weight: 21
---

Richten Sie das Logging ein, exportieren Sie Metriken an Prometheus oder einen
OpenTelemetry-Collector, und binden Sie die Liveness- und Readiness-Probes an.

## Logging konfigurieren {#configure-logging}

Teamster loggt nach stderr.

```yaml
log:
  level: info     # debug, info, warn oder error
  format: json    # text zum Lesen, json für eine Log-Pipeline
```

| Schlüssel | Umgebungsvariable | Standard |
| --- | --- | --- |
| `log.level` | `TEAMSTER_LOG_LEVEL` | `info` |
| `log.format` | `TEAMSTER_LOG_FORMAT` | `text` |

### Ursache eines Fehlers finden {#find-the-cause-of-an-error}

Jede Anfrage bekommt eine ID. Sie kommt als Header `X-Request-ID` zurück und steht als
`request_id` in jeder Logzeile dieser Anfrage. Ein `5xx` von der API oder einem Webhook antwortet
nur `{"error": "internal server error", "request_id": "…"}`, und eine Seite zeigt „Etwas ist
schiefgelaufen. Referenz: …“. Suchen Sie im Log nach dieser ID, um die Ursache zu finden.

Ein `4xx` sagt weiterhin, was zu beheben ist. Abgelehnte Webhook- und Bot-Anfragen werden mit dem
Grund als Warnung geloggt. `debug` fügt jeden Client-Fehler und jede angenommene Bot-Aktivität
hinzu.

## Metriken exportieren {#export-metrics}

Metriken sind standardmäßig aus. Schalten Sie sie ein und wählen Sie dann einen Prometheus-Scrape,
einen OTLP-Push oder beides.

{{< tabs >}}
{{< tab name="Prometheus-Scrape" >}}

```yaml
metrics:
  enabled: true
  addr: "127.0.0.1:9090"   # eigener Listener
  path: "/metrics"
  prometheus: true
```

Der Listener ist von dem der Verwaltungsoberfläche getrennt, nicht authentifiziert und lauscht
standardmäßig auf Loopback, weil die Attribute Routen, Vorlagen und Kanäle nennen. Binden Sie ihn
bewusst an eine Adresse, die Ihr Prometheus erreicht, etwa `:9090`. Er darf sich `server.addr`
nicht teilen.

{{< /tab >}}
{{< tab name="OTLP-Push" >}}

```yaml
metrics:
  enabled: true
  prometheus: false
  otlp-endpoint: "otel-collector.monitoring.svc:4318"
  otlp-protocol: "http"    # oder grpc
  otlp-interval: "60s"
  otlp-insecure: false     # true sendet ohne TLS
```

`otlp-endpoint` ist `host:port`. Reiner Push öffnet keinen Listener.

{{< /tab >}}
{{< tab name="Helm" >}}

```yaml
config:
  settings:
    metrics:
      enabled: true

metrics:
  serviceMonitor:
    enabled: true          # braucht die CRDs des Prometheus Operators
    labels:
      release: kube-prometheus-stack
```

Das Chart bindet den Listener an alle Schnittstellen, veröffentlicht einen Port `metrics` am
Service und lehnt eine Loopback-`addr` ab. Lassen Sie mit einer NetworkPolicy nur Prometheus zu.
Für OTLP setzen Sie `otlp-endpoint` unter `config.settings.metrics`; reiner Push erzeugt keinen
Port und keinen ServiceMonitor. Siehe [Helm-Values](../../reference/helm-values/).

{{< /tab >}}
{{< /tabs >}}

Jeder Schlüssel steht mit seiner Variablen `TEAMSTER_METRICS_*` unter
[Konfiguration](../../reference/configuration/). Teamster startet nicht mit `enabled: true`,
`prometheus: false` und ohne `otlp-endpoint`, weil dann nichts exportiert würde.

### Auf das alarmieren, was einen Menschen braucht {#alert-on-what-needs-a-person}

Alle Metriken stehen unter [Metriken](../../reference/metrics/). Diese hier bleiben bestehen, bis
jemand handelt:

| Beobachten | Bedeutet |
| --- | --- |
| `teamster.deliveries` mit outcome `app_missing` | Der Bot Connector hat einen Kanal-Post abgelehnt, fast immer, weil die Teams-App in diesem Team nicht installiert ist. |
| `teamster.destinations.without_app` über null | Ein Ziel liegt in einem Team, für das keine Installation der App bekannt ist. |
| `teamster.deliveries` mit outcome `failed` | Zustellungen schlagen fehl. Suchen Sie die Ursache im Log. |
| `teamster.webhook.receipts` mit state `refused` | Jemand sendet ein falsches Token. Siehe [Wenn ein Absender abgelehnt wird](../webhook-tokens/#when-a-sender-is-refused). |
| `teamster.audit.failed`, `teamster.audit.dropped` | Ereignisse des Änderungsprotokolls gingen verloren. Siehe [Änderungsprotokoll führen](../audit-trail/). |

In Prometheus werden Punkte zu Unterstrichen, und Zähler bekommen `_total`, zum Beispiel:

```promql
sum by (route) (increase(teamster_deliveries_total{outcome="app_missing"}[15m])) > 0
```

Go-Laufzeitmetriken tragen die Namen von OpenTelemetry, etwa `go_memory_used_bytes`, nicht die
`go_memstats_*`, die ein älteres Dashboard womöglich erwartet.

### Native Histogramme in Prometheus einschalten {#enable-native-histograms-in-prometheus}

Der Prometheus-Exporter liefert **native** Histogramme, die nur über Protobuf übertragen werden.
Starten Sie Prometheus mit:

```bash
prometheus --enable-feature=native-histograms
```

Der ServiceMonitor des Charts fragt bereits zuerst nach `PrometheusProto`.

{{< callout type="error" >}}
Ohne das Flag schlägt der Scrape nicht fehl. Er fällt auf Text zurück, und jedes Histogramm kommt
nur mit einem `+Inf`-Bucket, der Summe und der Anzahl an. Raten und Mittelwerte funktionieren,
`histogram_quantile` liefert `NaN`, und die Dashboards bleiben leer.
{{< /callout >}}

Diese Regel feuert genau dann, wenn das passiert ist:

```promql
count without(le) (http_server_request_duration_seconds_bucket) == 1
```

Zur Prüfung von Hand vergleichen Sie die beiden Formen. Die Textform zeigt wenige `_bucket`-Zeilen,
die Protobuf-Form ist das vollständige Histogramm:

```bash
curl -s localhost:9090/metrics | grep -c '_bucket'
curl -s -H 'Accept: application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited' \
  localhost:9090/metrics | wc -c
```

## Probes konfigurieren {#configure-the-probes}

| Pfad | Antwortet `200`, wenn |
| --- | --- |
| `GET /healthz` | Der Prozess läuft. |
| `GET /readyz` | Er bedienen kann: Er fährt nicht herunter, und die Datenbank antwortet. |

Beide sind nicht authentifiziert und beantworten auch `HEAD`. Sie laufen auf dem Haupt-Listener,
`server.addr`.

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: http }
readinessProbe:
  httpGet: { path: /readyz, port: http }
```

Das Helm-Chart setzt sie standardmäßig.

* Liveness ignoriert die Datenbank absichtlich. Ein Neustart bringt keine Datenbank zurück; er
  macht aus einem Ausfall eine Crash-Schleife.
* Readiness ignoriert Microsoft Graph absichtlich. Die Instanz aus der Rotation zu nehmen, wenn
  Graph ausfällt, würde die Verwaltungsoberfläche genau dann schließen, wenn Sie sehen wollen,
  warum die Zustellung scheitert.
* Readiness schlägt fehl, sobald ein Herunterfahren beginnt. So hört ein Rolling Update auf,
  Verkehr zu senden, bevor der Prozess aufhört, ihn anzunehmen.

Standardmäßig gibt es keine Startup-Probe. Führt der erste Start nach einem Upgrade eine lange
Migration aus, kann sie die Liveness-Probe überdauern und einen Neustart in Schleife auslösen. Die
`values.yaml` des Charts enthält für diesen Fall eine auskommentierte `startupProbe`.
