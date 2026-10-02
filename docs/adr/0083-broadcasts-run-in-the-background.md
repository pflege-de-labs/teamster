# 0083. A broadcast reaches everyone, in the background

* Status: Accepted
* Date: 2026-10-02

## Context

Office administrators need to send a notice to every colleague. With
[ADR 0082](0082-naming-people-takes-permission.md) they can name anyone, but listing a whole tenant
in `recipients` is impractical. `webhook.max-recipients` caps a list at 1000, and one request has
to finish within `server.write-timeout`. A tenant of thousands of people is many minutes of Bot
Connector calls, which no request can wait for and no single pod should have to finish.

## Decision

* **A flag, not a list.** The universal webhook takes `"broadcast": true`. It may not carry
  `recipients`, the `teamster_recipient` label, or a `state`: a broadcast is delivered once, and is
  never updated or closed.
* **Its own permission.** The Cedar action `broadcast` on `People::"*"` sits above `message` in
  the action hierarchy. Granting it, as the level **everyone**, includes naming anyone. A token
  needs the message level `everyone`, never above its creator's, checked against the creator on
  every use. Admins hold it.
* **Routes still decide.** The event is planned as usual. Deliveries to channels and linked chats
  are sent in the request. The addressed deliveries, those of routes that deliver to the people a
  message names, are stored with the event in `broadcasts`, and the webhook answers `202` with the
  broadcast's id. No addressed route means `422` with reason `no-addressed-route`.
* **The audience is everyone reachable.** That is every eligible directory user with an installed
  chat, plus every linked recipient the directory does not know, each once. It is read when the
  run starts. Blocked people are included, because blocked is no gate
  ([ADR 0026](0026-alerts-in-a-persons-chat.md)).
* **A background run, leased like a directory run.** Every replica looks for a waiting broadcast
  every 5 seconds. It claims one by compare-and-swap, sends in chunks of 25, up to
  `webhook.fanout-concurrency` at a time, and after each chunk writes a heartbeat with its counts
  and a cursor, the last person's key. A broadcast whose heartbeat is two minutes old is taken
  over from its cursor. A store error leaves it running for that takeover. Finished broadcasts are
  kept for 30 days.
* **Progress is visible.** `GET /webhook/broadcasts/{id}` answers a token of the same creator.
  `/admin/broadcasts` and `GET /api/broadcasts` list a user's own broadcasts, and everyone's for
  admins.

Alternatives considered:

* **Synchronous delivery with a cap.** It only works for small tenants, and a timeout halfway
  leaves no record of who got it.
* **A row per person.** It gives exact retries, at the price of a write per person before the first
  send. The cursor gives the same resumption at one write per chunk.
* **One lease for all broadcasts, like directory runs.** Broadcasts queue instead; the partial
  unique index would refuse the second one rather than wait.

## Consequences

* After a takeover, at most one chunk is sent twice. The heartbeat is written after the chunk, so a
  crash mid-chunk repeats it. Delivering each person exactly once is roadmap item 24.1.
* A person is counted once, by their worst outcome over the addressed routes: delivered,
  unreachable (blocked or removed the bot), or failed. A failed send is not retried.
* The audience is read when a run starts or is taken over. Someone who joins in between is
  reached only if a takeover reads them after the cursor.
* Additive migration: a new table, `broadcasts`. The previous release ignores it, and leaves queued
  broadcasts waiting until a new pod runs them.
