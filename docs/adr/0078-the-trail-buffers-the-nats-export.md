# 0078. The database trail can buffer the NATS export

* Status: Accepted
* Date: 2026-10-01

## Context

The NATS sink ([ADR 0071](0071-publish-audit-events-to-nats-jetstream.md)) is fed from an
in-memory queue. An outage longer than the queue drops events, and so does a restart while NATS is
unreachable. They remain in the database trail ([ADR 0070](0070-audit-configuration-changes-at-the-store.md))
but never reach the stream. Operators who treat the stream as the record want it complete across
NATS outages. The database already holds every event in order, and it can serve as the buffer.

## Decision

* **Opt-in.** With `audit.nats.backfill: true`, which needs `audit.database`, the NATS sink is not
  fed from the queue. An `audit.Relay` publishes from the trail instead.
* **The relay loop.** Every `audit.nats.backfill-interval` (default 2s), the relay reads the
  events after its cursor, oldest first, in batches of 100, and publishes them one by one:
  * after each acknowledged publish, it advances the cursor;
  * at the first failure, it stops and tries again on the next tick from the same place.

  An outage therefore delays events and loses none that retention keeps. Events from
  `teamster import`, which runs no relay, are published by the server's.
* **Where the cursor lives.** The cursor is `(occurred_at, id)` of the last event delivered,
  stored in `settings` as `audit.cursor.nats`.
* **The settle window.** The relay only publishes events older than `audit.nats.backfill-settle`
  (default 5s). An event is stamped before its insert commits, so a slower writer can commit an
  older event after a newer one. Without the window the cursor would pass it.
* **The first start.** When no cursor exists yet, it is placed at the newest event, or at now for
  an empty trail. Turning backfill on publishes what happens from then on, not the history.
* **Several replicas.** Every replica runs a relay. The cursor only advances by compare-and-swap,
  so a replica that loses the race stops for that step. The event it published twice carries the
  same `Nats-Msg-Id` and is dropped by the stream's duplicate window, so no lease is needed.
* **Shutdown.** Relays stop with the process. What they had not published waits in the trail.
* **The file sink** keeps its queue. It is local to the pod, which is never unreachable the way
  NATS is.

Alternatives considered:

* **Spooling the queue to disk.** A second buffer beside a trail that already holds every event,
  and per pod, so it is lost with the pod.
* **A lease so one replica relays.** It adds a moving part to save a duplicate that the stream
  already drops.
* **Pruning nothing the relay has not published.** That makes retention unbounded for as long as
  NATS is down, which is the default an operator should not be surprised by.

## Consequences

* An event reaches NATS up to `backfill-settle` plus `backfill-interval` after it happened.
* An outage longer than the database retention loses the events pruned meanwhile. Size
  `audit.retention-age` and `audit.retention-count` for the longest outage to ride out.
* Clock skew between replicas larger than the settle window can still let the cursor pass a late
  event. Keep the clocks synchronised, or raise the window.
* No schema change: the cursor is a `settings` row.
