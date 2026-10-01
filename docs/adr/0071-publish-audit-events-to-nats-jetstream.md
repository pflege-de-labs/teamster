# 0071. Publish audit events to NATS JetStream

* Status: Accepted
* Date: 2026-10-01

## Context

[ADR 0070](0070-audit-configuration-changes-at-the-store.md) records configuration changes in the
database and, optionally, in a JSON lines file. Some operators feed security events into an event
stream rather than a log pipeline. Their consumers expect each event once, filterable by kind, and
delivered even if the consumer was down when the change happened. NATS JetStream is the stream they
asked for.

The sink must not make Teamster depend on NATS being up. A NATS outage at start must not stop the
service, and an outage later must not slow requests. The recorder's queue already keeps external
sinks off the request path.

## Decision

We will add `audit.NATSSink`, an asynchronous sink behind the recorder's per-sink queue, configured
under `audit.nats`.

* **Subject and payload.** Each event is published to
  `<subject-prefix>.<resource type>.<action>`, such as `teamster.audit.Template.template.update`.
  The dots in an action stay subject levels, so `teamster.audit.*.route.>` selects every route
  change. Characters NATS reads as separators or wildcards are replaced with `_`. The payload is
  the same JSON the file sink writes.
* **Delivery.** A write uses JetStream publish, which waits for the stream's acknowledgement. It
  sets `Nats-Msg-Id` to the event id, which is a UUIDv7. A publish is retried twice with backoff
  inside the recorder's five-second write timeout. A retried publish that had in fact landed is
  dropped by the stream's duplicate window rather than stored twice.
* **Connection.** Teamster connects with `RetryOnFailedConnect` and unlimited reconnects. A server
  that is down at start delays events, which wait in the queue and are counted in
  `teamster.audit.dropped` once it is full. It does not delay the service, and readiness does not
  look at NATS.
* **Stream.** With `create-stream`, Teamster creates or updates `stream` to capture
  `<subject-prefix>.>` at start. A failure is logged, not fatal. Without `create-stream`, the
  stream is assumed to be managed elsewhere, which is the usual case where streams carry retention
  and replica settings.
* **Authentication.** `creds-file` (JWT and NKey seed), or credentials in the URL. A URL with
  credentials belongs in `TEAMSTER_AUDIT_NATS_URL` rather than the config file.

The JetStream test runs against a real server, the way the Postgres conformance suite does:
`make nats-up` and `make test-nats` locally, and a pinned `nats` image started by a step in CI.
JetStream needs the `-js` flag, which a service container cannot pass. The test fails rather than
skips when `CI` is set without `TEAMSTER_TEST_NATS_URL`.

Alternatives considered:

* **Core NATS publish.** It has no acknowledgement and no persistence, so an event is lost whenever
  no subscriber is connected. That loss is what JetStream exists to avoid.
* **Embedding `nats-server` in the tests.** It would add the server and its dependency tree to
  `go.mod`, and `go mod download` fetches that tree on every image build. That is the cost
  AGENTS.md declines for the goose CLI.
* **Publishing synchronously on the request path.** A slow broker would then slow every
  configuration change. The database row is the synchronous record; the stream is a copy.

## Consequences

* `github.com/nats-io/nats.go` becomes a direct dependency.
* The stream's duplicate window has to be at least as long as the retries take. The two-minute
  default comfortably exceeds the five seconds.
* An event dropped from a full queue reaches neither the file nor the stream. It is still in the
  database and counted in `teamster.audit.dropped`.
* CI starts one more container, and Renovate tracks its digest through a regex manager.
