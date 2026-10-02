---
title: Kubernetes
weight: 3
---

Install Teamster into a Kubernetes cluster with the Helm chart. The chart is published as an OCI
artifact at `oci://ghcr.io/pflege-de-labs/charts/teamster` and versioned independently of the
application; its `appVersion` names the Teamster release its image defaults to.

## Before you start

You need the Graph and bot registrations described in the
[quick start]({{< ref "/docs/getting-started/quick-start" >}}), Helm 3, and a namespace to install
into.

{{% steps %}}

### Install the chart

```bash
helm install teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --namespace monitoring --create-namespace \
  --set credentials.adminPassword=<admin-password> \
  --set credentials.graphClientSecret=<graph-client-secret> \
  --set config.settings.graph.tenant-id=<tenant-id> \
  --set config.settings.graph.client-id=<graph-client-id> \
  --set credentials.botClientSecret=<bot-client-secret> \
  --set config.settings.bot.tenant-id=<tenant-id> \
  --set config.settings.bot.client-id=<bot-client-id>
```

Teamster refuses to start without an admin login and a Graph credential. It starts without the
bot, but then nothing reaches Teams.

{{< callout type="warning" >}}
`--set` puts the secrets into the release history. Outside a demo, create a Secret yourself and
point `credentials.existingSecret` at it.
{{< /callout >}}

### Open the admin UI

```bash
kubectl --namespace monitoring port-forward svc/teamster 8080:8080
```

Open <http://localhost:8080/admin> and sign in as `admin` with the password you set. From here,
continue with the destination, route and token steps of the
[quick start]({{< ref "/docs/getting-started/quick-start" >}}).

{{% /steps %}}

## What the chart deploys

* **A StatefulSet with one replica** by default. State lives in a SQLite file on a volume, and
  SQLite takes a single writer, so the chart refuses `replicaCount` above 1 with `sqlite`.
* **A config Secret** built from `config.settings` and mounted at
  `/etc/xdg/teamster/config.yaml`. Its keys are Teamster's own hyphenated YAML keys.
* **A credentials Secret** built from `credentials`, injected as `TEAMSTER_*` environment
  variables. Teamster reads an environment variable only when no config file sets that key, so a
  secret never belongs under `config.settings`.
* **A Service** on port `8080`. Publish it with `ingress.enabled` or with Gateway API
  `httpRoute.external.enabled` and `httpRoute.internal.enabled`.

Changing either Secret rolls the pod.

## Run more than one replica

Point the release at a Postgres database. The chart then deploys a Deployment instead of a
StatefulSet, and any number of replicas share the database:

```bash
helm upgrade teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --namespace monitoring --reuse-values \
  --set database.driver=postgres \
  --set database.postgres.host=teamster-pg-rw \
  --set database.postgres.passwordFrom.secretName=teamster-pg-app \
  --set replicaCount=3
```

The chart deploys no Postgres of its own. It reads the password from the Secret your Postgres
operator or cloud provider already created.

## Next steps

* [Choose a storage backend](../../guides/storage/) and
  [move from SQLite to Postgres](../../guides/backup-and-migration/).
* [Send alerts from Alertmanager](../../guides/alertmanager/) inside the cluster.
* Every chart value: [Helm values](../../reference/helm-values/).
