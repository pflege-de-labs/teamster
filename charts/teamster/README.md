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

## How the pod is run

| `workload.kind` | Storage | When to use it |
| --- | --- | --- |
| `StatefulSet` (default) | `volumeClaimTemplate` managed by the chart | The normal case. The claim outlives the pod and follows it when it is rescheduled. |
| `Deployment` | `persistence.existingClaim`, or `persistence.create=true` | A cluster where claims are provisioned separately from the workload. |

Both run exactly one replica, and the chart refuses `replicaCount` above 1: teamster keeps its
state in SQLite, which takes a single writer, so a second replica would serve a database of its
own. A `Deployment` rolls with the `Recreate` strategy, because a `ReadWriteOnce` volume admits
one pod at a time.

`persistence.enabled=false` runs on an `emptyDir`. Every template, destination, route and active
alert is then lost when the pod restarts — a demo setting, nothing more.

### There is no remote-database mode yet

`database.driver` accepts `sqlite` only. `postgres` is rejected at render time with a message
rather than quietly ignored, so a values file written against a future release fails loudly
instead of starting a server that keeps its state somewhere the operator did not expect. Adding
Postgres is an application change first — see the [roadmap](../../docs/roadmap.md).

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

Set `credentials.existingSecret` to a secret you manage — sealed-secrets, external-secrets,
whatever the cluster uses — and the chart creates none. That secret has to use the key names in
the right-hand column. `config.existingSecret` does the same for the config file, which then needs
a `config.yaml` key.

Both secrets are hashed into pod annotations, so changing either rolls the pod.

## Exposing the service

Enable exactly one of the two, or neither and reach the service through `kubectl port-forward`:

* `ingress.enabled=true` — a `networking.k8s.io/v1` Ingress.
* `httpRoute.enabled=true` — a Gateway API `HTTPRoute`, attached to the gateways in
  `httpRoute.parentRefs`. Needs the Gateway API CRDs and a controller.

Both front the same Service, which carries the admin UI, the admin API and the two webhook
endpoints. There is no way to publish the webhooks without publishing `/admin` — put an
authenticating proxy or a network policy in front if that matters, and note that
`/webhook/*` authenticates with the `X-Teamster-Token` header rather than with a session.

When OIDC is configured, `config.settings.auth.oidc-redirect-url` has to be the externally
reachable `/admin/auth/callback` URL as registered with the provider. The chart cannot derive it:
the hostname belongs to the Ingress or the HTTPRoute, and the scheme to whatever terminates TLS.

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
for, the port they name, the config mount and the data volume. A probe dropped
because a values key was emptied or a helper refactored is the sort of change
that deploys happily and is noticed at three in the morning.

## Values

The [values.yaml](values.yaml) comments are the reference. The ones most often changed:

| Key | Default | What it does |
| --- | --- | --- |
| `workload.kind` | `StatefulSet` | `StatefulSet` or `Deployment`. |
| `replicaCount` | `1` | Must stay 1. |
| `persistence.enabled` | `true` | Off means an `emptyDir` and no durable state. |
| `persistence.size` | `1Gi` | Size of the SQLite volume. |
| `persistence.storageClass` | `""` | Empty uses the cluster default. |
| `image.tag` | `""` | Defaults to the chart's `appVersion`. |
| `config.settings` | see values | The config file, in teamster's own key names. |
| `credentials.existingSecret` | `""` | Use a secret you manage. |
| `service.port` | `8080` | Port the Service publishes. |
| `ingress.enabled` / `httpRoute.enabled` | `false` | How the service is published. |
| `extraObjects` | `{}` | Extra resources, as a map or a list. |

## Testing a release

```bash
helm test teamster --namespace monitoring
```

Fetches `/readyz` through the Service and fails if the instance does not report itself ready.
