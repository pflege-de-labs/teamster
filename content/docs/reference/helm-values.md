---
title: Helm values
weight: 6
---

Every value of the `teamster` Helm chart, chart version 0.10.0 (application 0.11.0), grouped by
top-level key in the order of `values.yaml`.

## Replicas

| Key | Default | Description |
| --- | --- | --- |
| `replicaCount` | `1` | Pods to run. Must be `1` with `database.driver=sqlite`; any number with `postgres`. |

## image

| Key | Default | Description |
| --- | --- | --- |
| `image.repository` | `ghcr.io/pflege-de-labs/teamster` | Image repository. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `image.tag` | `""` | Image tag. Empty uses the chart's `appVersion`. |
| `imagePullSecrets` | `[]` | Pull secrets for the pod. |

## Names and labels

| Key | Default | Description |
| --- | --- | --- |
| `nameOverride` | `""` | Replaces the chart name in resource names. |
| `fullnameOverride` | `""` | Replaces the full resource name. |
| `commonLabels` | `{}` | Labels on every resource, pods included. A component's own labels win over these; the chart's selector labels win over both. |

## workload

| Key | Default | Description |
| --- | --- | --- |
| `workload.kind` | `""` | `StatefulSet` or `Deployment`. Empty derives it from the driver: `StatefulSet` for `sqlite`, `Deployment` for `postgres`. |
| `workload.annotations` | `{}` | Annotations on the Deployment or StatefulSet. |
| `workload.labels` | `{}` | Labels on the Deployment or StatefulSet; pods take `podLabels`. |
| `workload.updateStrategy` | `{type: RollingUpdate}` | StatefulSet only. |
| `workload.strategy` | `{}` | Deployment only. Empty derives `Recreate` for `sqlite` and a surging `RollingUpdate` for `postgres`. |

## database

| Key | Default | Description |
| --- | --- | --- |
| `database.driver` | `sqlite` | `sqlite` for one pod with a file on a volume, `postgres` for several pods sharing a database. The chart deploys no Postgres. |
| `database.sqlite.fileName` | `teamster.db` | File name below `persistence.mountPath`. The chart writes the full path into the config as `database.path`. |
| `database.postgres.host` | `""` | Postgres host. Required with `postgres`. |
| `database.postgres.port` | `5432` | Postgres port. |
| `database.postgres.dbname` | `teamster` | Database name. |
| `database.postgres.user` | `teamster` | Database user. |
| `database.postgres.sslmode` | `require` | libpq `sslmode`. |
| `database.postgres.sslrootcert` | `""` | CA file path inside the container. Mount it with `volumes` and `volumeMounts`. |
| `database.postgres.passwordFrom.secretName` | `""` | Secret holding the Postgres password. Mutually exclusive with `credentials.databasePassword`. |
| `database.postgres.passwordFrom.key` | `password` | Key in that secret. |
| `database.maxOpenConns` | `0` | Maximum open connections; `0` lets the backend choose. |
| `database.maxIdleConns` | `0` | Maximum idle connections; `0` lets the backend choose. |
| `database.connMaxLifetime` | `""` | How long a pooled connection may be reused, as a duration. Empty lets the backend choose. |

With `postgres`, the chart needs a password from `credentials.databasePassword`,
`database.postgres.passwordFrom`, or `TEAMSTER_DATABASE_POSTGRES_PASSWORD` in
`credentials.existingSecret`.

## persistence

The volume carrying the SQLite file. Ignored with `database.driver=postgres`.

| Key | Default | Description |
| --- | --- | --- |
| `persistence.enabled` | `true` | Off runs on an `emptyDir`; all state is lost when the pod restarts. |
| `persistence.mountPath` | `/data` | Where the volume is mounted. |
| `persistence.existingClaim` | `""` | A claim you manage instead of one from the chart. |
| `persistence.create` | `false` | Deployment only: render a PersistentVolumeClaim. A StatefulSet always uses a `volumeClaimTemplate`. |
| `persistence.keepOnUninstall` | `true` | Deployment only: keep the claim when the release is uninstalled. |
| `persistence.accessModes` | `[ReadWriteOnce]` | Access modes of the claim. |
| `persistence.size` | `1Gi` | Size of the claim. |
| `persistence.storageClass` | `""` | Storage class. Empty uses the cluster default. |
| `persistence.annotations` | `{}` | Annotations on the claim. |
| `persistence.labels` | `{}` | Labels on the claim. |
| `persistence.retentionPolicy` | `{}` | StatefulSet only, Kubernetes 1.27 or later: `whenDeleted`, `whenScaled`. |

## serviceAccount

| Key | Default | Description |
| --- | --- | --- |
| `serviceAccount.create` | `true` | Create a service account. |
| `serviceAccount.automount` | `true` | Mount its API credentials. |
| `serviceAccount.annotations` | `{}` | Annotations on the service account. |
| `serviceAccount.labels` | `{}` | Labels on the service account. |
| `serviceAccount.name` | `""` | Name to use. Empty with `create` generates one from the full name. |

## Pod

| Key | Default | Description |
| --- | --- | --- |
| `podAnnotations` | `{}` | Annotations on the pod. |
| `podLabels` | `{}` | Labels on the pod. |
| `podSecurityContext` | `runAsNonRoot: true`, `runAsUser: 65532`, `runAsGroup: 65532`, `fsGroup: 65532`, `seccompProfile.type: RuntimeDefault` | Pod security context. The image runs as uid 65532. |
| `securityContext` | `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]` | Container security context. |
| `terminationGracePeriodSeconds` | `30` | Must exceed `config.settings.server.shutdown-timeout`. |
| `nodeSelector` | `{}` | Node selector. |
| `tolerations` | `[]` | Tolerations. |
| `affinity` | `{}` | Affinity. |
| `topologySpreadConstraints` | `[]` | Topology spread constraints. |
| `priorityClassName` | `""` | Priority class. |
| `resources` | `{}` | Container resources. |

## service

| Key | Default | Description |
| --- | --- | --- |
| `service.type` | `ClusterIP` | Service type. |
| `service.port` | `8080` | Port the Service publishes. |
| `service.nodePort` | `""` | Node port, with `type: NodePort` only. |
| `service.annotations` | `{}` | Annotations on the Service. |
| `service.labels` | `{}` | Labels on the Service and, under a StatefulSet, its headless twin. |

The metrics port is added to the Service whenever the metrics listener exists.

## metrics

Scraping of the metrics listener. Needs `config.settings.metrics.enabled`.

| Key | Default | Description |
| --- | --- | --- |
| `metrics.serviceMonitor.enabled` | `false` | Render a ServiceMonitor. Requires the Prometheus Operator's CRDs and a listener to scrape. |
| `metrics.serviceMonitor.namespace` | `""` | Namespace of the ServiceMonitor. Empty is the release namespace. |
| `metrics.serviceMonitor.labels` | `{}` | Labels the operator's `serviceMonitorSelector` looks for. |
| `metrics.serviceMonitor.annotations` | `{}` | Annotations on the ServiceMonitor. |
| `metrics.serviceMonitor.interval` | `""` | Scrape interval. Empty uses the Prometheus global. |
| `metrics.serviceMonitor.scrapeTimeout` | `""` | Scrape timeout. Empty uses the Prometheus global. |
| `metrics.serviceMonitor.scrapeProtocols` | `[PrometheusProto, OpenMetricsText1.0.0, PrometheusText0.0.4]` | `PrometheusProto` first carries native histograms. |
| `metrics.serviceMonitor.honorLabels` | `false` | `honorLabels` of the endpoint. |
| `metrics.serviceMonitor.relabelings` | `[]` | Relabelings. |
| `metrics.serviceMonitor.metricRelabelings` | `[]` | Metric relabelings. |

## config

The config file, mounted at `/etc/xdg/teamster/config.yaml`.

| Key | Default | Description |
| --- | --- | --- |
| `config.existingSecret` | `""` | A Secret you manage with a `config.yaml` key, used instead of `config.settings`. |
| `config.settings` | see below | Written verbatim into `config.yaml`. Keys are Teamster's own; see [Configuration](../configuration/). |

`config.settings` sets these by default. Every other key takes the application default.
`database.*` is written by the chart from the [`database`](#database) values; with `sqlite`,
`database.path` is always `persistence.mountPath` joined with `database.sqlite.fileName`.

| Key | Default |
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
| `config.settings.metrics.addr` | `:9090`, not the application's loopback default |
| `config.settings.metrics.path` | `/metrics` |

The chart refuses to render when:

| Condition | Reason |
| --- | --- |
| `webhook.token`, `admin.password`, `graph.client-secret` or `database.postgres.password` is set under `config.settings` | A config file value beats the environment variable from `credentials`. |
| `metrics.addr` is loopback while the Prometheus exporter is on | Nothing outside the pod can reach it. |
| `metrics.addr` and `server.addr` name the same port | The second listener cannot bind. |
| `metrics.enabled` with `prometheus: false` and no `otlp-endpoint` | Collected and exported nowhere. |
| `audit.nats.create-stream` with `nack.stream.enabled` | Both would manage the stream. |
| `metrics.serviceMonitor.enabled` without a Prometheus listener | Nothing to scrape. |
| `database.driver` other than `sqlite` or `postgres` | Unknown driver. |
| `workload.kind` other than `StatefulSet`, `Deployment` or empty | Unsupported kind. |
| `replicaCount` above 1 with `sqlite` | SQLite takes a single writer. |
| `workload.kind=Deployment` with `sqlite` and neither `persistence.existingClaim` nor `persistence.create` | The file needs a volume that outlives the pod. |
| `postgres` without `database.postgres.host` | The chart deploys no Postgres. |
| `postgres` without a password source, or with both `credentials.databasePassword` and `passwordFrom` | See [database](#database). |
| `postgres` with `persistence.existingClaim` | Postgres keeps no local state. |
| `pdb.minAvailable` and `pdb.maxUnavailable` both set | Only one may be set. |

## credentials

Delivered as `TEAMSTER_*` environment variables from a Secret.

| Key | Default | Environment variable | Description |
| --- | --- | --- | --- |
| `credentials.existingSecret` | `""` | — | A Secret you manage, with the variable names as keys. The chart creates none. |
| `credentials.webhookToken` | `""` | `TEAMSTER_WEBHOOK_TOKEN` | Optional deployment-wide webhook token. |
| `credentials.adminUsername` | `admin` | `TEAMSTER_ADMIN_USERNAME` | Local admin login. Required unless `existingSecret` is set. |
| `credentials.adminPassword` | `""` | `TEAMSTER_ADMIN_PASSWORD` | Local admin password. Required unless `existingSecret` is set. |
| `credentials.graphClientSecret` | `""` | `TEAMSTER_GRAPH_CLIENT_SECRET` | Graph registration secret. Required unless `existingSecret` is set. |
| `credentials.oidcClientSecret` | `""` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` | OIDC client secret. Omit for a public client using PKCE. |
| `credentials.botClientSecret` | `""` | `TEAMSTER_BOT_CLIENT_SECRET` | Bot registration secret. No channel or chat delivery works without it. |
| `credentials.brokerTokenEncryptionKey` | `""` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` | 32 random bytes, base64, for `auth.broker`. |
| `credentials.databasePassword` | `""` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` | Postgres password. Mutually exclusive with `database.postgres.passwordFrom`. |
| `credentials.extra` | `{}` | as named | Further `TEAMSTER_*` variables in the same Secret. |

Changing either Secret rolls the pod.

## Container extras

| Key | Default | Description |
| --- | --- | --- |
| `sidecars` | `[]` | Containers appended after Teamster's, such as an OTLP collector. |
| `extraArgs` | `[]` | Arguments appended after the `serve` command. |
| `env` | `[]` | Extra environment variables. |
| `envFrom` | `[]` | Extra `envFrom` sources. |
| `volumes` | `[]` | Extra pod volumes. |
| `volumeMounts` | `[]` | Extra container mounts. |

## Probes

| Key | Default | Description |
| --- | --- | --- |
| `livenessProbe` | `httpGet /healthz` on `http`, `initialDelaySeconds: 5`, `periodSeconds: 20` | Liveness probe. Does not check the database. |
| `readinessProbe` | `httpGet /readyz` on `http`, `initialDelaySeconds: 2`, `periodSeconds: 10` | Readiness probe. Fails while the database is unreachable or shutdown has begun. |
| `startupProbe` | `{}` | No startup probe. Set one when a migration may outlast the liveness probe. |

## ingress

| Key | Default | Description |
| --- | --- | --- |
| `ingress.enabled` | `false` | Publish the admin UI and the webhooks through one Ingress. |
| `ingress.className` | `""` | Ingress class. |
| `ingress.labels` | `{}` | Labels on the Ingress. |
| `ingress.annotations` | `{}` | Annotations on the Ingress. |
| `ingress.hosts` | `[{host: teamster.local, paths: [{path: /, pathType: Prefix}]}]` | Hosts and paths. |
| `ingress.tls` | `[]` | TLS entries. |

## httpRoute

Gateway API HTTPRoutes, an alternative to the Ingress. Require the Gateway API CRDs and a
controller.

| Key | Default | Description |
| --- | --- | --- |
| `httpRoute.external.enabled` | `false` | Route the self-authenticating endpoints. While `internal` is off, it routes everything. |
| `httpRoute.external.labels` | `{}` | Labels on the route. |
| `httpRoute.external.annotations` | `{}` | Annotations on the route. |
| `httpRoute.external.parentRefs` | Gateway `gateway`, section `http` | Gateways the route attaches to. |
| `httpRoute.external.hostnames` | `[teamster.local]` | Host names. |
| `httpRoute.external.matches` | `/webhook/alertmanager`, `/webhook/universal` (exact), `/teamsv2` (prefix), `/bot/messages` (exact) | Paths routed. Do not add `/admin` or `/api`. |
| `httpRoute.external.filters` | `[]` | Filters. |
| `httpRoute.external.timeouts` | `{}` | Timeouts. |
| `httpRoute.internal.enabled` | `false` | Route the admin UI and API separately from the webhooks. |
| `httpRoute.internal.labels` | `{}` | Labels on the route. |
| `httpRoute.internal.annotations` | `{}` | Annotations on the route. |
| `httpRoute.internal.parentRefs` | Gateway `gateway`, section `http` | Gateways the route attaches to. |
| `httpRoute.internal.hostnames` | `[teamster.local]` | Host names. |
| `httpRoute.internal.matches` | `/` (prefix) | Paths routed. |
| `httpRoute.internal.filters` | `[]` | Filters. |
| `httpRoute.internal.timeouts` | `{}` | Timeouts. |

## pdb

Rendered only with `database.driver=postgres` and `replicaCount` above 1.

| Key | Default | Description |
| --- | --- | --- |
| `pdb.enabled` | `true` | Render a PodDisruptionBudget. |
| `pdb.minAvailable` | `1` | Minimum available pods. Not together with `maxUnavailable`. |
| `pdb.maxUnavailable` | `""` | Maximum unavailable pods. |
| `pdb.labels` | `{}` | Labels on the budget. |

## nack

JetStream resources for the audit export, as [NACK](https://github.com/nats-io/nack) custom
resources. Require NACK's CRDs and controller.

| Key | Default | Description |
| --- | --- | --- |
| `nack.apiVersion` | `jetstream.nats.io/v1beta2` | API version of the NACK resources. |
| `nack.account.create` | `false` | Render an Account. |
| `nack.account.name` | `""` | Name of the Account to render, or to reference with `create: false`. Empty is the release full name. |
| `nack.account.labels` | `{}` | Labels on the Account. |
| `nack.account.annotations` | `{}` | Annotations on the Account. |
| `nack.account.spec` | `{}` | `Account.spec`. |
| `nack.stream.enabled` | `false` | Render a Stream. Name and subjects follow `config.settings.audit.nats`. |
| `nack.stream.resourceName` | `""` | `metadata.name`. Empty is `<fullname>-audit`. |
| `nack.stream.labels` | `{}` | Labels on the Stream. |
| `nack.stream.annotations` | `{}` | Annotations on the Stream. |
| `nack.stream.spec` | `{}` | `Stream.spec`, merged over `storage: file`, `replicas: 1`, `duplicateWindow: 2m`, `preventDelete: true`. |
| `nack.consumers` | `{}` | Durable consumers keyed by durable name. Each value is a `Consumer.spec`; `metadata.name` is `<fullname>-<key>`. |

## tests

| Key | Default | Description |
| --- | --- | --- |
| `tests.image.repository` | `busybox` | Image used by `helm test`. |
| `tests.image.tag` | `1.38` | Its tag. |
| `tests.image.pullSecrets` | `[]` | Its pull secrets. |

## extraObjects

| Key | Default | Description |
| --- | --- | --- |
| `extraObjects` | `{}` | Extra resources, as a map (merged across values files) or a list (replaced). Each entry is an object or a string, run through `tpl`. |

## See also

* [Kubernetes](../../getting-started/kubernetes/)
* [Configuration](../configuration/)
