---
title: Record an audit trail
weight: 20
---

Record who changed what in Teamster's configuration, read it in the admin UI, and export it to a
file or NATS JetStream for your SIEM.

Auditing is off until you configure it. With it on, every change is recorded: templates,
destinations, routes, webhook endpoints, access tokens, grants, groups, the default template and
destination, linked chats, and install runs someone asked for. An event says who made the change,
how they signed in (`session`, `basic` or `cli`), the request id, and the record before and after.
Credentials are never included.

## Keep the trail in the database

```yaml
audit:
  database: true
```

Or `TEAMSTER_AUDIT_DATABASE=true`. Admins then read the trail at **/admin/audit**, newest first,
filtered by actor, action, type, id and time.

Scripts read the same trail with `GET /api/audit`:

```bash
curl -u <admin-user>:<admin-password> \
  'http://localhost:8080/api/audit?type=Route&since=2026-10-01T00:00:00Z&limit=100'
```

| Parameter | Filters by |
| --- | --- |
| `actor`, `action`, `type`, `id` | Who, what was done, the resource type and id |
| `since`, `until` | Time, RFC 3339 |
| `limit` | Page size, default 50 |
| `cursor`, `at` | The next page: pass back the `next` object of the previous answer |

### Bound how much is kept

The values shown are the defaults:

```yaml
audit:
  retention-age: "2160h"    # forget events older than 90 days; 0 keeps them regardless of age
  retention-count: 100000   # keep at most this many events; 0 is no limit
  prune-interval: "1h"
```

Both limits apply, whichever is reached first.

## Also write events to a file

```yaml
audit:
  file: "/data/audit.jsonl"   # "-" is stdout
```

Each event is one JSON line. On Kubernetes, `-` hands the events to your log pipeline.

## Publish events to NATS JetStream

{{% steps %}}

### Set the server

```yaml
audit:
  database: true
  nats:
    url: "nats://nats:4222"
    subject-prefix: "teamster.audit"
    stream: "TEAMSTER_AUDIT"
    create-stream: false      # true creates or updates the stream at start
    creds-file: ""            # JWT and NKey seed, if the server wants them
```

Put a URL that carries credentials in `TEAMSTER_AUDIT_NATS_URL` rather than in the file.

### Create the stream

Either set `create-stream: true` and let Teamster create the stream, or declare it yourself, for
example [with NACK](#declare-the-stream-with-nack). The stream must capture
`<subject-prefix>.>`.

### Subscribe

Each event is published to `<subject-prefix>.<resource type>.<action>`, for example
`teamster.audit.Route.route.delete`. Subscribe to `teamster.audit.>` for everything, or to
`teamster.audit.*.route.>` for route changes. The event id is the `Nats-Msg-Id`, so the stream
drops a retried duplicate.

{{% /steps %}}

A NATS server that is down does not stop Teamster. Events wait in a queue of `audit.queue-size`
(default `1024`) per sink, and are dropped and counted when it is full. `audit.nats.timeout`
(default `5s`) bounds connecting and creating the stream.

A sink receives only the events recorded while it is configured. Turning it on later does not send
older events.

### Ride out NATS outages with backfill

With backfill, the database trail is the buffer: events are published from it in order, and the
position is kept in the database. Teamster catches up once NATS is back, including after a restart.

```yaml
audit:
  database: true            # required
  nats:
    url: "nats://nats:4222"
    backfill: true
    backfill-interval: "2s" # how often to look for new events
    backfill-settle: "5s"   # how old an event must be before it is published
```

* Turning backfill on starts from the newest event. History is not sent.
* Changes made by `teamster import` are published by the running server.
* An outage longer than the retention loses what retention pruned meanwhile. Size `retention-age`
  and `retention-count` for the longest outage you want to survive.

See [ADR 0071](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0071-publish-audit-events-to-nats-jetstream.md)
and [ADR 0078](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0078-the-trail-buffers-the-nats-export.md).

## Declare the stream with NACK

On Kubernetes, the Helm chart can declare the stream and durable consumers as
[NACK](https://github.com/nats-io/nack) resources.

{{< callout type="warning" >}}
Install NACK first. The chart installs neither its CRDs nor its JetStream controller. Without the
CRDs the install fails; without the controller the resources are never reconciled.
{{< /callout >}}

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm install nack nats/nack --namespace nats \
  --set jetstream.enabled=true --set jetstream.nats.url=nats://nats.nats.svc:4222
```

Then, in the Teamster release's values:

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

* The `Stream` takes its name and subjects from `audit.nats.stream` and `subject-prefix`.
  `nack.stream.spec` is merged over the defaults: file storage, one replica, a two-minute
  `duplicateWindow`, and `preventDelete: true`, which keeps the history when the release is
  uninstalled.
* Each key of `nack.consumers` becomes a durable `Consumer` of that stream.
* `nack.account.create` renders an `Account` from `nack.account.spec`, and `nack.account.name`
  references an existing one. NACK reconciles `Account` resources only with
  `jetstream.controlLoop=true`.
* The chart refuses `nack.stream.enabled` together with `audit.nats.create-stream`, because NACK
  would undo what Teamster sets.

Deliver Teamster's own NATS credentials in `credentials.extra.TEAMSTER_AUDIT_NATS_URL`, or mount a
creds file with `volumes` and `volumeMounts` and name it in `audit.nats.creds-file`. All chart keys
are in [Helm values](../../reference/helm-values/).

## Watch for lost events

A failed audit write never fails the change. It is logged and counted in `teamster.audit.failed`,
and events dropped from a full queue are counted in `teamster.audit.dropped`, both by `sink`.
Alert on either; see [Monitor Teamster](../observability/).
