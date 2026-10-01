# 0070. Audit configuration changes at the store boundary

* Status: Accepted
* Date: 2026-10-01

## Context

Teamster records nothing about who changed its configuration. A route that stops delivering, a
destination pointed at the wrong channel or a token revoked by mistake can be traced only from
request logs, which do not show what the record held before. Per-record ownership, groups and
self-service tokens are planned (see the [roadmap](../roadmap.md)). Those make permission changes
part of daily operation, so the trail has to exist before they arrive.

Configuration is written through several doors: the admin forms, the JSON API, `POST
/api/config/import` and `teamster import`. All of them reach `store.Store`. An import, and a route
save checked inside a serializable transaction, write several records in one transaction that may
roll back. A serializable transaction may also run its function more than once.

Operators want three destinations for the trail: a file for their log shipper, an event stream (NATS
JetStream), and a view in the admin UI. The database copy must be bounded by age or by count.

## Decision

We will audit at the store boundary. `audit.Wrap(store, recorder)` returns a `store.Store` that
overrides every configuration write: templates, destinations, routes, webhook endpoints, access
tokens, grants, the default template and destination settings, recipient link and unlink, and
manually requested directory runs.

* **What an event holds.** Each override reads the record first and calls the inner store. On
  success it emits an `AuditEvent` with these fields:
  * `action`, such as `template.update`
  * the resource type and id
  * JSON snapshots `before` and `after`
  * the actor
  * the request id

  Secrets never reach the trail, because the models' JSON tags already omit token digests.
* **Writes that changed nothing.** An update or delete of a record that was not there emits nothing,
  although the store accepts it.
* **Who the actor is.** `withPrincipal` puts the actor in the context, with `via` set to `session`
  or `basic`. The logging middleware adds the request id. `teamster import` acts as the
  operating-system user with `via` set to `cli`. A write with no actor in its context, such as
  seeding the presets, is recorded as `teamster`/`system`.
* **Transactions.** Inside `WithTx` and `WithSerializableTx` events are buffered and delivered only
  after the commit. A rolled-back change, including a dry-run import, leaves no record. A
  serializable retry discards the previous attempt's buffer.
* **Sinks.** A `Recorder` delivers to sinks through an interface:
  * The database sink writes before `Record` returns, so `/admin/audit` shows a change at once.
  * Every other sink has a bounded queue (`audit.queue-size`). A full queue drops the event and
    counts it in `teamster.audit.dropped`.
  * A failed write is logged and counted in `teamster.audit.failed`.
  * The `audit.file` sink appends JSON lines, or writes to stdout when set to `-`.
  * NATS JetStream follows as its own sink and ADR.
* **When a sink fails.** Recording fails open: the change has already committed, so an audit
  failure never fails the request.
* **Retention.** `audit.retention-age` and `audit.retention-count` each bound the database trail,
  and `0` switches a limit off. Every replica runs the pruner every `audit.prune-interval`. Deleting
  is idempotent, so the replicas need no lease.
* **Who may read it.** `/admin/audit` and `GET /api/audit` are the `administer` action on `Audit`,
  so only admins can read the trail. They filter by actor, action, resource type and id, and page
  with a keyset cursor `(occurred_at, id)`. Event ids are UUIDv7, so they sort by time.

Alternatives considered:

* **A call in every handler.** Simple, but the form, API, import and CLI paths would each need it.
  A missed path would be silent, and every handler would need its own before/after read.
* **Database triggers.** These know nothing of the actor or the request. They would also have to be
  written twice, once per dialect, outside sqlc's checking.
* **Writing the audit row inside the change's transaction.** This fails closed: no record, no
  change. That suits permission changes, which arrive later and will do it for themselves. For
  ordinary configuration it would turn a full audit table into an outage.

## Consequences

* A new configuration write must be overridden in `internal/audit/store.go`, or it goes unaudited.
  The decorator's tests list every audited method.
* Every audited update and delete costs one extra read.
* `audit_events` is a new table, added by SQLite `0023` and Postgres `0020`. The previous release
  ignores it.
* The trail fails open. A database outage that rejects the audit insert but accepts the change
  itself is visible only through the error log and `teamster.audit.failed`.
* The file sink rotates with `copytruncate`. Teamster does not reopen the file on a signal.
