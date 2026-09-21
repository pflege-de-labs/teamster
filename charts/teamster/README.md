# teamster Helm chart

Deploys [teamster](https://github.com/pflege-de-labs/teamster), a webhook bridge that routes
Alertmanager and universal alerts to Microsoft Teams.

## Install

```bash
helm install teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --namespace monitoring --create-namespace \
  --set credentials.webhookToken=... \
  --set credentials.adminPassword=... \
  --set credentials.graphClientSecret=... \
  --set config.settings.graph.tenant-id=... \
  --set config.settings.graph.client-id=...
```

Those five values have no default: teamster refuses to start without a webhook token, an admin
login and a Graph credential. Use `credentials.existingSecret` instead of the three `--set`
credentials in anything but a demo.

## Choosing a backend

| `database.driver` | Shape | When |
| --- | --- | --- |
| `sqlite` (default) | One pod, state in a file on a volume | A single instance with no other runtime dependency. |
| `postgres` | Any number of pods sharing one database | More than one instance, and upgrades with no gap. |

`workload.kind` is derived from the driver unless you set it: a StatefulSet for `sqlite`, whose
file needs a claim that follows the pod, and a Deployment for `postgres`, which keeps nothing
locally. Both can be overridden; the combinations that cannot work are refused at render time
rather than deployed.

With `sqlite` the chart refuses `replicaCount` above 1 — SQLite takes a single writer, so a second
replica would serve a database of its own — and a Deployment rolls with `Recreate`, because a
`ReadWriteOnce` volume admits one pod. With `postgres` there is no such limit: replicas roll with
`maxUnavailable: 0` and a surge pod, and a multi-replica release gets a PodDisruptionBudget.

`persistence` applies to `sqlite` only and is ignored under `postgres`, where nothing is written
outside `/tmp`. `persistence.enabled=false` runs on an `emptyDir`: every template, destination,
route and active alert is lost when the pod restarts, which is a demo setting and nothing more.

### Bring your own Postgres

**This chart does not deploy a Postgres, and that is deliberate.** Bundling a single-pod
`postgresql` subchart on a PVC would make "teamster is highly available" mean "teamster now depends
on something less available than the StatefulSet it replaced", while looking like the opposite. HA
Postgres in Kubernetes is an operator's job or a managed service.

What the chart does instead is read the secret that operator already created:

```yaml
database:
  driver: postgres
  postgres:
    host: teamster-pg-rw
    passwordFrom:
      secretName: teamster-pg-app   # created by the Postgres operator
      key: password
replicaCount: 3
```

A cluster can be declared alongside the release with `extraObjects`, so one `helm install` still
does everything:

```yaml
extraObjects:
  postgres: |
    apiVersion: postgresql.cnpg.io/v1
    kind: Cluster
    metadata:
      name: teamster-pg
    spec:
      instances: 3
      storage:
        size: 5Gi
      bootstrap:
        initdb:
          database: teamster
          owner: teamster
```

### Migrations

Migrations run when the process opens the database, guarded by an advisory lock so two instances
starting together cannot both apply them. Set `config.settings.database.migrate=verify` to make it
a step of its own instead — an instance then refuses to serve a database that is behind, naming
`teamster migrate up`. Deployments whose application role has no DDL rights want that, and can run
the command from a Job declared in `extraObjects`.

There is no `helm.sh/hook` Job for it. Hooks render before the config and credentials Secrets they
would need, and a failed hook leaves the release `failed` with the old pods still serving, which
reads as "the upgrade did nothing" rather than "the migration failed".

## Configuration and credentials

Two things reach the container, and the split matters.

`config.settings` is written verbatim into a Secret and mounted at `/etc/xdg/teamster/config.yaml`,
the system-wide XDG location teamster searches at startup. The keys are teamster's own hyphenated
flag names, so anything in [`config.example.yaml`](../../config.example.yaml) can be set here.
`database.path` is the exception: the chart owns it, because it has to agree with the volume
mount.

`credentials` becomes a second Secret whose keys are environment variable names, injected with
`envFrom`. Teamster reads an environment variable **only when no config file sets that key**, so
these four must not also appear under `config.settings`:

| Value | Environment variable |
| --- | --- |
| `credentials.webhookToken` | `TEAMSTER_WEBHOOK_TOKEN` |
| `credentials.adminUsername` | `TEAMSTER_ADMIN_USERNAME` |
| `credentials.adminPassword` | `TEAMSTER_ADMIN_PASSWORD` |
| `credentials.graphClientSecret` | `TEAMSTER_GRAPH_CLIENT_SECRET` |
| `credentials.oidcClientSecret` (optional) | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` |
| `credentials.botClientSecret` (optional) | `TEAMSTER_BOT_CLIENT_SECRET` |
| `credentials.databasePassword` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` |

Set `credentials.existingSecret` to a secret you manage — sealed-secrets, external-secrets,
whatever the cluster uses — and the chart creates none. That secret has to use the key names in
the right-hand column. `config.existingSecret` does the same for the config file, which then needs
a `config.yaml` key.

Both secrets are hashed into pod annotations, so changing either rolls the pod.

## Exposing the service

Enable one of the two, or neither and reach the service through `kubectl port-forward`:

* `ingress.enabled=true` — a `networking.k8s.io/v1` Ingress. One resource, every path, the way
  the Service has always been reachable: there is no way to publish the webhooks without
  publishing `/admin`. Put an authenticating proxy or a network policy in front if that matters.
* `httpRoute.external.enabled=true` and, separately, `httpRoute.internal.enabled=true` — Gateway
  API `HTTPRoute`s. Needs the Gateway API CRDs and a controller.

  `httpRoute.external` carries the paths that authenticate themselves — `/webhook/alertmanager`,
  `/webhook/universal`, `/teamsv2/*`, `/bot/messages` — a shared token, a URL token, or
  Microsoft's own signature on a bot activity, never a session. `httpRoute.internal` carries
  everything else: the admin UI and API, gated by a session or basic auth.

  **`httpRoute.internal` is off by default, and while it is, `httpRoute.external` alone reaches
  both** — its rule set folds in `httpRoute.internal`'s catch-all, so a deployment that wants one
  route keeps exactly the all-in-one behaviour the chart used to have with a single `enabled`
  flag. Turn `httpRoute.internal` on to put the admin interface on its own Gateway or hostname —
  a private listener, a VPN-only DNS name — separate from whatever reaches the webhooks from the
  internet. There is no fallback the other way: `httpRoute.internal.enabled=true` with
  `httpRoute.external` left off does not expose the webhooks, because that would change what a
  deployment publishes based on a flag named for the opposite purpose.

  This split exists for HTTPRoute only. Ingress keeps its single-resource, all-paths shape; if the
  split matters to you, use Gateway API.

Either way, `/webhook/*` authenticates with the `X-Teamster-Token` header rather than with a
session, and `/bot/messages` — reachable only when the bot is configured — authenticates with
Microsoft's own signature.

When OIDC is configured, `config.settings.auth.oidc-redirect-url` has to be the externally
reachable `/admin/auth/callback` URL as registered with the provider. The chart cannot derive it:
the hostname belongs to the Ingress or the HTTPRoute carrying the admin interface, and the scheme
to whatever terminates TLS.

## extraObjects

Deploys arbitrary resources alongside the release. Two forms:

* **Map** (recommended) — keys deep-merge across several `-f values.yaml` files, so entries from
  different files combine.
* **List** — simpler, but Helm replaces lists across values files, so the last `-f` wins.

Each entry is either a YAML object or a string, and both go through `tpl`. A template expression
in the object form has to sit inside a quoted scalar, or the values file stops being YAML; the
string form templates whole blocks:

```yaml
extraObjects:
  webhook-url:
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: "{{ include \"teamster.fullname\" . }}-webhook"
    data:
      url: "http://{{ include \"teamster.fullname\" . }}:{{ .Values.service.port }}/webhook/alertmanager"
  labelled-secret: |
    apiVersion: v1
    kind: Secret
    metadata:
      name: {{ include "teamster.fullname" . }}-extra
      labels:
        {{- include "teamster.labels" . | nindent 8 }}
    type: Opaque
    stringData:
      namespace: {{ .Release.Namespace }}
```

## Probes

The defaults use the endpoints teamster serves for exactly this: `httpGet /healthz` for liveness,
`httpGet /readyz` for readiness. Both are unauthenticated. Readiness fails as soon as a shutdown
begins, so a rolling update stops traffic arriving before the process stops accepting it, and it
fails when the store does not answer — which is also why liveness does not look at the store:
restarting on an unreachable database turns an outage into a crash loop.

`helm test` asks for `/readyz` through the Service.

There is no `startupProbe` by default: starting is opening the database and
running its migrations, which liveness covers. `values.yaml` carries one
commented out for the case that does not — the first start after an upgrade
that rebuilds a table, where a long migration can outlast the liveness probe
and turn into a restart loop that never finishes it.

`make chart-lint` renders the chart from each of the `ci/` value sets and
asserts what comes out: both probes present on whichever workload was asked
for, exactly one of each, the port they name, the config mount, the data
volume, and a metrics port that either appears on both the container and the
Service or on neither. It also checks that the guards still refuse a loopback
metrics address, a metrics port colliding with the server's, and a
ServiceMonitor with no listener to scrape. A probe dropped because a values key
was emptied or a helper refactored is the sort of change that deploys happily
and is noticed at three in the morning.

## Metrics

Off, as in the application. There are two shapes, and the chart renders only what the one you chose
needs — a port exists when something listens on it, and not otherwise.

### Scraped: the Prometheus exporter

A **second listener**, published as a `metrics` port on the Service:

```yaml
config:
  settings:
    metrics:
      enabled: true        # prometheus: true is the default

metrics:
  serviceMonitor:
    enabled: true          # needs the Prometheus operator's CRDs
    labels:
      release: kube-prometheus-stack
```

Two settings, because they answer different questions: `config.settings.metrics` is teamster's own
configuration, written into `config.yaml` verbatim; `metrics.serviceMonitor` is how Kubernetes is
told to scrape it. The container port, the Service port and the ServiceMonitor's `port: metrics` all
come from `config.settings.metrics.addr`, so they cannot drift.

The listener **asks for no credentials**, exactly like the probes. The application binds it to
`127.0.0.1` for that reason; in a pod that reaches nothing, so the chart binds all interfaces and
**refuses a loopback `addr`** rather than publishing a Service port that resolves to silence. The
boundary here is the pod network — use a NetworkPolicy admitting only Prometheus.

### Pushed: OTLP to a collector

Set an endpoint, and turn the exporter off if scraping is not also wanted:

```yaml
config:
  settings:
    metrics:
      enabled: true
      prometheus: false
      otlp-endpoint: otel-collector.monitoring.svc:4318
      otlp-protocol: http
```

Push-only renders **no container port, no Service port and no ServiceMonitor**: nothing listens, so
publishing anything would point at a closed socket. `addr` is never read in this shape, and the
loopback guard does not apply to it.

Both at once is fine — leave `prometheus: true` and set an endpoint.

### A collector in the pod

`sidecars` puts one beside the service. A sidecar shares the pod's network namespace, which is what
makes `localhost` the right endpoint there — nothing leaves the pod on the way to it:

```yaml
config:
  settings:
    metrics:
      enabled: true
      prometheus: false
      otlp-endpoint: localhost:4318
      otlp-insecure: true

sidecars:
  - name: otel-collector
    image: otel/opentelemetry-collector-contrib:0.140.0
    args: ["--config=/etc/otel/config.yaml"]
    volumeMounts:
      - name: otel-config
        mountPath: /etc/otel

volumes:
  - name: otel-config
    configMap:
      name: my-collector-config
```

[`ci/otlp-values.yaml`](ci/otlp-values.yaml) is that shape end to end, collector config included.

`otlp-insecure: true` is plaintext, which is what a loopback hop inside one pod wants. Across the
cluster to a collector Service, leave it off.

### What the chart refuses

* a loopback `addr` while the exporter is on — unreachable, as above
* `addr` naming the same port as `server.addr`, which the application rejects on startup
* `enabled: true` with `prometheus: false` and no `otlp-endpoint` — collected and exported nowhere,
  which the application also rejects on startup
* `metrics.serviceMonitor.enabled` with no listener to scrape

### Native histograms

The exporter emits native histograms, which travel over **protobuf only**. The ServiceMonitor asks
for `PrometheusProto` first, but Prometheus itself must be started with:

```text
--enable-feature=native-histograms
```

Without it the scrape does **not** fail. It falls back to text, and every histogram arrives with its
buckets gone — `_sum` and `_count` and a single `+Inf`. Rates and averages keep working,
`histogram_quantile` returns `NaN`, and the heatmaps are empty. The dashboard renders, which is
worse than an outage. The rule that catches it:

```promql
count without(le) (http_server_request_duration_seconds_bucket) == 1
```

## Values

The [values.yaml](values.yaml) comments are the reference. The ones most often changed:

| Key | Default | What it does |
| --- | --- | --- |
| `database.driver` | `sqlite` | `sqlite` or `postgres`. |
| `database.postgres.host` | `""` | Required under `postgres`. |
| `database.postgres.passwordFrom` | `{}` | Read the password from a secret somebody else owns. |
| `workload.kind` | `""` | Derived from the driver; set to override. |
| `replicaCount` | `1` | Must stay 1 under `sqlite`; any number under `postgres`. |
| `pdb.enabled` | `true` | A disruption budget, for multi-replica `postgres` only. |
| `persistence.enabled` | `true` | Off means an `emptyDir` and no durable state. |
| `persistence.size` | `1Gi` | Size of the SQLite volume. |
| `persistence.storageClass` | `""` | Empty uses the cluster default. |
| `image.tag` | `""` | Defaults to the chart's `appVersion`. |
| `config.settings` | see values | The config file, in teamster's own key names. |
| `credentials.existingSecret` | `""` | Use a secret you manage. |
| `credentials.databasePassword` | `""` | The Postgres password, if not using `passwordFrom`. |
| `service.port` | `8080` | Port the Service publishes. |
| `ingress.enabled` | `false` | Publish everything through one Ingress. |
| `httpRoute.external.enabled` | `false` | Publish the webhooks; also everything else, unless `internal` is on. |
| `httpRoute.internal.enabled` | `false` | Split the admin interface onto its own route. |
| `config.settings.metrics.enabled` | `false` | Opens the metrics listener and publishes its port. |
| `config.settings.metrics.otlp-endpoint` | unset | Push metrics to a collector instead of, or beside, being scraped. |
| `metrics.serviceMonitor.enabled` | `false` | Render a ServiceMonitor for the Prometheus operator. |
| `sidecars` | `[]` | Extra containers in the pod, e.g. an OTLP collector. |
| `extraObjects` | `{}` | Extra resources, as a map or a list. |

## Testing a release

```bash
helm test teamster --namespace monitoring
```

Fetches `/readyz` through the Service and fails if the instance does not report itself ready.
