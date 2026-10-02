---
title: Helm-Werte
weight: 6
---

Alle Werte des Helm-Charts `teamster` in Chart-Version 0.10.0 (Anwendung 0.11.0), gruppiert nach
Schlüssel der obersten Ebene in der Reihenfolge von `values.yaml`.

## Replikate {#replicas}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `replicaCount` | `1` | Anzahl der Pods. Muss mit `database.driver=sqlite` `1` sein; mit `postgres` beliebig. |

## image {#image}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `image.repository` | `ghcr.io/pflege-de-labs/teamster` | Image-Repository. |
| `image.pullPolicy` | `IfNotPresent` | Pull-Policy des Images. |
| `image.tag` | `""` | Image-Tag. Leer verwendet die `appVersion` des Charts. |
| `imagePullSecrets` | `[]` | Pull-Secrets für den Pod. |

## Namen und Labels {#names-and-labels}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `nameOverride` | `""` | Ersetzt den Chart-Namen in Ressourcennamen. |
| `fullnameOverride` | `""` | Ersetzt den vollständigen Ressourcennamen. |
| `commonLabels` | `{}` | Labels an jeder Ressource, Pods eingeschlossen. Die eigenen Labels einer Komponente haben Vorrang vor diesen; die Selector-Labels des Charts haben Vorrang vor beiden. |

## workload {#workload}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `workload.kind` | `""` | `StatefulSet` oder `Deployment`. Leer leitet die Art vom Treiber ab: `StatefulSet` für `sqlite`, `Deployment` für `postgres`. |
| `workload.annotations` | `{}` | Annotationen am Deployment oder StatefulSet. |
| `workload.labels` | `{}` | Labels am Deployment oder StatefulSet; Pods erhalten `podLabels`. |
| `workload.updateStrategy` | `{type: RollingUpdate}` | Nur StatefulSet. |
| `workload.strategy` | `{}` | Nur Deployment. Leer leitet `Recreate` für `sqlite` und ein `RollingUpdate` mit Surge für `postgres` ab. |

## database {#database}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `database.driver` | `sqlite` | `sqlite` für einen Pod mit einer Datei auf einem Volume, `postgres` für mehrere Pods mit gemeinsamer Datenbank. Das Chart stellt kein Postgres bereit. |
| `database.sqlite.fileName` | `teamster.db` | Dateiname unterhalb von `persistence.mountPath`. Das Chart schreibt den vollständigen Pfad als `database.path` in die Konfiguration. |
| `database.postgres.host` | `""` | Postgres-Host. Mit `postgres` erforderlich. |
| `database.postgres.port` | `5432` | Postgres-Port. |
| `database.postgres.dbname` | `teamster` | Name der Datenbank. |
| `database.postgres.user` | `teamster` | Datenbankbenutzer. |
| `database.postgres.sslmode` | `require` | `sslmode` von libpq. |
| `database.postgres.sslrootcert` | `""` | Pfad der CA-Datei im Container. Binden Sie sie mit `volumes` und `volumeMounts` ein. |
| `database.postgres.passwordFrom.secretName` | `""` | Secret mit dem Postgres-Passwort. Schließt `credentials.databasePassword` aus. |
| `database.postgres.passwordFrom.key` | `password` | Schlüssel in diesem Secret. |
| `database.maxOpenConns` | `0` | Höchstzahl offener Verbindungen; `0` überlässt die Wahl dem Backend. |
| `database.maxIdleConns` | `0` | Höchstzahl untätiger Verbindungen; `0` überlässt die Wahl dem Backend. |
| `database.connMaxLifetime` | `""` | Wie lange eine Verbindung aus dem Pool wiederverwendet werden darf, als Dauer. Leer überlässt die Wahl dem Backend. |

Mit `postgres` braucht das Chart ein Passwort aus `credentials.databasePassword`,
`database.postgres.passwordFrom` oder `TEAMSTER_DATABASE_POSTGRES_PASSWORD` in
`credentials.existingSecret`.

## persistence {#persistence}

Das Volume mit der SQLite-Datei. Wird mit `database.driver=postgres` ignoriert.

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `persistence.enabled` | `true` | Ausgeschaltet läuft Teamster auf einem `emptyDir`; jeder Zustand geht beim Neustart des Pods verloren. |
| `persistence.mountPath` | `/data` | Wo das Volume eingebunden wird. |
| `persistence.existingClaim` | `""` | Ein Claim, den Sie selbst verwalten, statt eines Claims aus dem Chart. |
| `persistence.create` | `false` | Nur Deployment: einen PersistentVolumeClaim rendern. Ein StatefulSet verwendet immer ein `volumeClaimTemplate`. |
| `persistence.keepOnUninstall` | `true` | Nur Deployment: den Claim behalten, wenn das Release deinstalliert wird. |
| `persistence.accessModes` | `[ReadWriteOnce]` | Zugriffsmodi des Claims. |
| `persistence.size` | `1Gi` | Größe des Claims. |
| `persistence.storageClass` | `""` | Storage-Class. Leer verwendet den Standard des Clusters. |
| `persistence.annotations` | `{}` | Annotationen am Claim. |
| `persistence.labels` | `{}` | Labels am Claim. |
| `persistence.retentionPolicy` | `{}` | Nur StatefulSet, ab Kubernetes 1.27: `whenDeleted`, `whenScaled`. |

## serviceAccount {#serviceaccount}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `serviceAccount.create` | `true` | Einen Service-Account anlegen. |
| `serviceAccount.automount` | `true` | Seine API-Zugangsdaten einbinden. |
| `serviceAccount.annotations` | `{}` | Annotationen am Service-Account. |
| `serviceAccount.labels` | `{}` | Labels am Service-Account. |
| `serviceAccount.name` | `""` | Zu verwendender Name. Leer erzeugt mit `create` einen aus dem vollständigen Namen. |

## Pod {#pod}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `podAnnotations` | `{}` | Annotationen am Pod. |
| `podLabels` | `{}` | Labels am Pod. |
| `podSecurityContext` | `runAsNonRoot: true`, `runAsUser: 65532`, `runAsGroup: 65532`, `fsGroup: 65532`, `seccompProfile.type: RuntimeDefault` | Security-Context des Pods. Das Image läuft als UID 65532. |
| `securityContext` | `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]` | Security-Context des Containers. |
| `terminationGracePeriodSeconds` | `30` | Muss `config.settings.server.shutdown-timeout` übersteigen. |
| `nodeSelector` | `{}` | Node-Selector. |
| `tolerations` | `[]` | Tolerations. |
| `affinity` | `{}` | Affinität. |
| `topologySpreadConstraints` | `[]` | Topology Spread Constraints. |
| `priorityClassName` | `""` | Priority-Class. |
| `resources` | `{}` | Ressourcen des Containers. |

## service {#service}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `service.type` | `ClusterIP` | Service-Typ. |
| `service.port` | `8080` | Port, den der Service veröffentlicht. |
| `service.nodePort` | `""` | Node-Port, nur mit `type: NodePort`. |
| `service.annotations` | `{}` | Annotationen am Service. |
| `service.labels` | `{}` | Labels am Service und, unter einem StatefulSet, an seinem Headless-Gegenstück. |

Der Metrik-Port wird dem Service hinzugefügt, sobald der Metrik-Listener existiert.

## metrics {#metrics}

Scraping des Metrik-Listeners. Setzt `config.settings.metrics.enabled` voraus.

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `metrics.serviceMonitor.enabled` | `false` | Einen ServiceMonitor rendern. Erfordert die CRDs des Prometheus Operators und einen Listener zum Scrapen. |
| `metrics.serviceMonitor.namespace` | `""` | Namespace des ServiceMonitors. Leer ist der Namespace des Releases. |
| `metrics.serviceMonitor.labels` | `{}` | Labels, nach denen der `serviceMonitorSelector` des Operators sucht. |
| `metrics.serviceMonitor.annotations` | `{}` | Annotationen am ServiceMonitor. |
| `metrics.serviceMonitor.interval` | `""` | Scrape-Intervall. Leer verwendet den globalen Wert von Prometheus. |
| `metrics.serviceMonitor.scrapeTimeout` | `""` | Scrape-Timeout. Leer verwendet den globalen Wert von Prometheus. |
| `metrics.serviceMonitor.scrapeProtocols` | `[PrometheusProto, OpenMetricsText1.0.0, PrometheusText0.0.4]` | `PrometheusProto` an erster Stelle überträgt native Histogramme. |
| `metrics.serviceMonitor.honorLabels` | `false` | `honorLabels` des Endpunkts. |
| `metrics.serviceMonitor.relabelings` | `[]` | Relabelings. |
| `metrics.serviceMonitor.metricRelabelings` | `[]` | Metric-Relabelings. |

## config {#config}

Die Konfigurationsdatei, eingebunden unter `/etc/xdg/teamster/config.yaml`.

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `config.existingSecret` | `""` | Ein Secret, das Sie selbst verwalten, mit einem Schlüssel `config.yaml`; wird statt `config.settings` verwendet. |
| `config.settings` | siehe unten | Wird unverändert in `config.yaml` geschrieben. Die Schlüssel sind die von Teamster; siehe [Konfiguration](../configuration/). |

`config.settings` setzt standardmäßig diese Werte. Jeder andere Schlüssel erhält den Standardwert
der Anwendung. `database.*` schreibt das Chart aus den Werten unter [`database`](#database); mit
`sqlite` ist `database.path` immer `persistence.mountPath` verbunden mit
`database.sqlite.fileName`.

| Schlüssel | Standard |
| --- | --- |
| `config.settings.server.addr` | `:8080` |
| `config.settings.server.shutdown-timeout` | `15s` |
| `config.settings.server.read-timeout` | `15s` |
| `config.settings.server.write-timeout` | `60s` |
| `config.settings.server.idle-timeout` | `120s` |
| `config.settings.server.external-url` | `""` |
| `config.settings.graph.tenant-id` | `""` |
| `config.settings.graph.client-id` | `""` |
| `config.settings.graph.base-url` | `https://graph.microsoft.com/v1.0` |
| `config.settings.graph.timeout-sec` | `10` |
| `config.settings.bot` | `{}` |
| `config.settings.auth` | `{}` |
| `config.settings.ui.language` | `en` |
| `config.settings.metrics.enabled` | `false` |
| `config.settings.metrics.prometheus` | `true` |
| `config.settings.metrics.addr` | `:9090`, nicht der Loopback-Standard der Anwendung |
| `config.settings.metrics.path` | `/metrics` |

Das Chart verweigert das Rendern, wenn:

| Bedingung | Grund |
| --- | --- |
| `webhook.token`, `admin.password`, `graph.client-secret` oder `database.postgres.password` unter `config.settings` gesetzt ist | Ein Wert in der Konfigurationsdatei hat Vorrang vor der Umgebungsvariablen aus `credentials`. |
| `metrics.addr` Loopback ist, während der Prometheus-Exporter eingeschaltet ist | Von außerhalb des Pods ist er nicht erreichbar. |
| `metrics.addr` und `server.addr` denselben Port nennen | Der zweite Listener kann sich nicht binden. |
| `metrics.enabled` mit `prometheus: false` und ohne `otlp-endpoint` gesetzt ist | Erfasst, aber nirgends exportiert. |
| `audit.nats.create-stream` zusammen mit `nack.stream.enabled` gesetzt ist | Beide würden den Stream verwalten. |
| `metrics.serviceMonitor.enabled` ohne Prometheus-Listener gesetzt ist | Es gibt nichts zu scrapen. |
| `database.driver` weder `sqlite` noch `postgres` ist | Unbekannter Treiber. |
| `workload.kind` weder `StatefulSet` noch `Deployment` noch leer ist | Nicht unterstützte Art. |
| `replicaCount` mit `sqlite` größer als 1 ist | SQLite erlaubt nur einen Schreiber. |
| `workload.kind=Deployment` mit `sqlite` und weder `persistence.existingClaim` noch `persistence.create` gesetzt ist | Die Datei braucht ein Volume, das den Pod überdauert. |
| `postgres` ohne `database.postgres.host` gewählt ist | Das Chart stellt kein Postgres bereit. |
| `postgres` ohne Passwortquelle gewählt ist, oder mit `credentials.databasePassword` und `passwordFrom` zugleich | Siehe [database](#database). |
| `postgres` mit `persistence.existingClaim` gewählt ist | Postgres hält keinen lokalen Zustand. |
| `pdb.minAvailable` und `pdb.maxUnavailable` beide gesetzt sind | Nur eines darf gesetzt sein. |

## credentials {#credentials}

Werden als `TEAMSTER_*`-Umgebungsvariablen aus einem Secret übergeben.

| Schlüssel | Standard | Umgebungsvariable | Beschreibung |
| --- | --- | --- | --- |
| `credentials.existingSecret` | `""` | — | Ein Secret, das Sie selbst verwalten, mit den Variablennamen als Schlüsseln. Das Chart legt dann keines an. |
| `credentials.webhookToken` | `""` | `TEAMSTER_WEBHOOK_TOKEN` | Optionales Webhook-Token für die gesamte Installation. |
| `credentials.adminUsername` | `admin` | `TEAMSTER_ADMIN_USERNAME` | Anmeldename des lokalen Administrators. Erforderlich, sofern `existingSecret` nicht gesetzt ist. |
| `credentials.adminPassword` | `""` | `TEAMSTER_ADMIN_PASSWORD` | Passwort des lokalen Administrators. Erforderlich, sofern `existingSecret` nicht gesetzt ist. |
| `credentials.graphClientSecret` | `""` | `TEAMSTER_GRAPH_CLIENT_SECRET` | Secret der Graph-Registrierung. Erforderlich, sofern `existingSecret` nicht gesetzt ist. |
| `credentials.oidcClientSecret` | `""` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` | OIDC-Client-Secret. Weglassen für einen öffentlichen Client mit PKCE. |
| `credentials.botClientSecret` | `""` | `TEAMSTER_BOT_CLIENT_SECRET` | Secret der Bot-Registrierung. Ohne es funktioniert keine Zustellung in Kanäle oder Chats. |
| `credentials.brokerTokenEncryptionKey` | `""` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` | 32 zufällige Bytes, Base64, für `auth.broker`. |
| `credentials.databasePassword` | `""` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` | Postgres-Passwort. Schließt `database.postgres.passwordFrom` aus. |
| `credentials.extra` | `{}` | wie benannt | Weitere `TEAMSTER_*`-Variablen im selben Secret. |

Eine Änderung an einem der beiden Secrets startet den Pod neu.

## Container-Extras {#container-extras}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `sidecars` | `[]` | Container, die nach dem von Teamster angehängt werden, etwa ein OTLP-Collector. |
| `extraArgs` | `[]` | Argumente, die nach dem Befehl `serve` angehängt werden. |
| `env` | `[]` | Zusätzliche Umgebungsvariablen. |
| `envFrom` | `[]` | Zusätzliche `envFrom`-Quellen. |
| `volumes` | `[]` | Zusätzliche Volumes des Pods. |
| `volumeMounts` | `[]` | Zusätzliche Mounts des Containers. |

## Probes {#probes}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `livenessProbe` | `httpGet /healthz` auf `http`, `initialDelaySeconds: 5`, `periodSeconds: 20` | Liveness-Probe. Prüft die Datenbank nicht. |
| `readinessProbe` | `httpGet /readyz` auf `http`, `initialDelaySeconds: 2`, `periodSeconds: 10` | Readiness-Probe. Schlägt fehl, solange die Datenbank nicht erreichbar ist oder das Herunterfahren begonnen hat. |
| `startupProbe` | `{}` | Keine Startup-Probe. Setzen Sie eine, wenn eine Migration länger dauern kann, als die Liveness-Probe zulässt. |

## ingress {#ingress}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `ingress.enabled` | `false` | Verwaltungsoberfläche und Webhooks über ein gemeinsames Ingress veröffentlichen. |
| `ingress.className` | `""` | Ingress-Class. |
| `ingress.labels` | `{}` | Labels am Ingress. |
| `ingress.annotations` | `{}` | Annotationen am Ingress. |
| `ingress.hosts` | `[{host: teamster.local, paths: [{path: /, pathType: Prefix}]}]` | Hosts und Pfade. |
| `ingress.tls` | `[]` | TLS-Einträge. |

## httpRoute {#httproute}

HTTPRoutes der Gateway API, eine Alternative zum Ingress. Erfordern die CRDs der Gateway API und
einen Controller.

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `httpRoute.external.enabled` | `false` | Die Endpunkte routen, die sich selbst authentifizieren. Solange `internal` ausgeschaltet ist, routet diese Route alles. |
| `httpRoute.external.labels` | `{}` | Labels an der Route. |
| `httpRoute.external.annotations` | `{}` | Annotationen an der Route. |
| `httpRoute.external.parentRefs` | Gateway `gateway`, Abschnitt `http` | Gateways, an die sich die Route anhängt. |
| `httpRoute.external.hostnames` | `[teamster.local]` | Hostnamen. |
| `httpRoute.external.matches` | `/webhook/alertmanager`, `/webhook/universal` (exakt), `/teamsv2` (Präfix), `/bot/messages` (exakt) | Geroutete Pfade. Fügen Sie `/admin` oder `/api` nicht hinzu. |
| `httpRoute.external.filters` | `[]` | Filter. |
| `httpRoute.external.timeouts` | `{}` | Timeouts. |
| `httpRoute.internal.enabled` | `false` | Verwaltungsoberfläche und API getrennt von den Webhooks routen. |
| `httpRoute.internal.labels` | `{}` | Labels an der Route. |
| `httpRoute.internal.annotations` | `{}` | Annotationen an der Route. |
| `httpRoute.internal.parentRefs` | Gateway `gateway`, Abschnitt `http` | Gateways, an die sich die Route anhängt. |
| `httpRoute.internal.hostnames` | `[teamster.local]` | Hostnamen. |
| `httpRoute.internal.matches` | `/` (Präfix) | Geroutete Pfade. |
| `httpRoute.internal.filters` | `[]` | Filter. |
| `httpRoute.internal.timeouts` | `{}` | Timeouts. |

## pdb {#pdb}

Wird nur mit `database.driver=postgres` und `replicaCount` größer als 1 gerendert.

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `pdb.enabled` | `true` | Ein PodDisruptionBudget rendern. |
| `pdb.minAvailable` | `1` | Mindestzahl verfügbarer Pods. Nicht zusammen mit `maxUnavailable`. |
| `pdb.maxUnavailable` | `""` | Höchstzahl nicht verfügbarer Pods. |
| `pdb.labels` | `{}` | Labels am Budget. |

## nack {#nack}

JetStream-Ressourcen für den Audit-Export, als Custom Resources von
[NACK](https://github.com/nats-io/nack). Erfordern die CRDs und den Controller von NACK.

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `nack.apiVersion` | `jetstream.nats.io/v1beta2` | API-Version der NACK-Ressourcen. |
| `nack.account.create` | `false` | Einen Account rendern. |
| `nack.account.name` | `""` | Name des zu rendernden Accounts oder, mit `create: false`, des referenzierten. Leer ist der vollständige Name des Releases. |
| `nack.account.labels` | `{}` | Labels am Account. |
| `nack.account.annotations` | `{}` | Annotationen am Account. |
| `nack.account.spec` | `{}` | `Account.spec`. |
| `nack.stream.enabled` | `false` | Einen Stream rendern. Name und Subjects folgen `config.settings.audit.nats`. |
| `nack.stream.resourceName` | `""` | `metadata.name`. Leer ist `<fullname>-audit`. |
| `nack.stream.labels` | `{}` | Labels am Stream. |
| `nack.stream.annotations` | `{}` | Annotationen am Stream. |
| `nack.stream.spec` | `{}` | `Stream.spec`, gemergt über `storage: file`, `replicas: 1`, `duplicateWindow: 2m`, `preventDelete: true`. |
| `nack.consumers` | `{}` | Durable Consumer, nach Durable-Namen geschlüsselt. Jeder Wert ist eine `Consumer.spec`; `metadata.name` ist `<fullname>-<key>`. |

## tests {#tests}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `tests.image.repository` | `busybox` | Von `helm test` verwendetes Image. |
| `tests.image.tag` | `1.38` | Sein Tag. |
| `tests.image.pullSecrets` | `[]` | Seine Pull-Secrets. |

## extraObjects {#extraobjects}

| Schlüssel | Standard | Beschreibung |
| --- | --- | --- |
| `extraObjects` | `{}` | Zusätzliche Ressourcen, als Map (über Values-Dateien hinweg zusammengeführt) oder als Liste (ersetzt). Jeder Eintrag ist ein Objekt oder ein String und läuft durch `tpl`. |

## Siehe auch {#see-also}

* [Kubernetes](../../getting-started/kubernetes/)
* [Konfiguration](../configuration/)
