# 0085. Pace Bot Connector calls and back off together on throttling

* Status: Accepted
* Date: 2026-10-03

## Context

Every channel post, chat message, edit and install check goes out through the Bot Connector
with the bot's own credential ([ADR 0045](0045-channel-delivery-through-the-bot.md)).
Teams throttles that bot as a whole. A single conversation takes at most 7 calls a second and
1800 an hour, and the bot shares one budget across the tenant. The bottleneck is global rather
than specific to broadcasts.

Until now the only limits were per-request concurrency caps (`webhook.fanout-concurrency`,
`bot.install-concurrency`). Nothing slowed a broadcast down, and nothing kept two webhooks from
sending at the same moment. A 429 from the Connector counted as an ordinary failure. It was not
retried: a person in a broadcast simply went without, and a synchronous sender got a 502.
Graph retried 429 and 503 only for directory and install calls, and the team and channel
listings did not retry at all. No metric told throttling apart from a stuck run (roadmap 23.6).

Replicas share nothing but the database, so a limit that holds exactly across all of them needs
either a round trip to the database on every call or a queue such as NATS.

## Decision

We will pace every Bot Connector call through one pacer per process, chosen by
`bot.pacing.strategy`, and retry the calls the Connector refused without acting on them.

* **Strategy `process`**, the default and so far the only one. It uses a token bucket for the
  process (`rate`, `burst`), a token bucket for each conversation (`conversation-rate`,
  `conversation-burst`), and a shared pause. A call waits for its conversation's bucket first and
  then for the global one, so a call held back in one conversation does not sit on a global token.
  The key is the conversation for a send or edit, the channel for a new channel post, and the
  person for opening a personal chat.
* **Shared backoff.** A 429 sets a pause that every later call waits out before taking a token,
  so one throttled call holds back the calls behind it instead of each one hitting the same limit
  in turn. A shorter Retry-After never cuts a longer pause short.
* **Retries.** A 429 or a 503 is sent again up to `bot.pacing.retries` times. The wait honours
  Retry-After, falls back to 1s, 2s and 4s, and is capped at `max-retry-wait`. No other failure is
  retried: after a 502 or a 504 the message may already have been delivered, and a retry would post
  it twice.
* **Graph.** The team, channel and installed-app listings go through the same retrying request
  as the directory calls.
* **Context.** Every wait ends with the caller's context. A synchronous webhook that would wait
  past its deadline fails as transient (a 502), and the sender retries, as before.
* **Metrics.** `teamster.throttled{api}` counts each 429 and 503 from Graph or the Connector.
  `teamster.pacing.wait` records how long each call waited for the pacer.

Alternatives considered, and kept as roadmap strategies rather than rejected:

* **`database`.** One budget for all replicas, shared through Postgres. It is exact, but it costs
  a round trip on every call and needs a lease of its own.
* **`nats`.** Every send queued on a JetStream work queue and drained by one paced consumer. This
  also gives each person exactly once (roadmap 24.1), but it makes NATS a dependency for
  delivery rather than only for audit export.

## Consequences

* With one replica, nothing sends faster than the configured budget, and a 429 slows everything
  down instead of losing messages. With several replicas, each paces itself. The operator divides
  the tenant's budget by the replica count, and the shared backoff absorbs an overshoot.
* A large fan-out takes longer and may run into `server.write-timeout`. Broadcasts are not
  affected, because they run in the background.
* Retries hold a fan-out worker for as long as the backoff lasts. That is bounded by
  `retries × max-retry-wait` and by the caller's context.
* A broadcast chunk gets half of the two-minute heartbeat window
  ([ADR 0083](0083-broadcasts-run-in-the-background.md)). Without that limit, backoff could keep a
  chunk running past the heartbeat, and a second replica would take the broadcast over while the
  first was still sending. A person still waiting when the time runs out is counted as failed.
* The per-conversation limiters are kept in a map. Idle ones are dropped once it holds 10000.
* There is no migration and no schema change. Existing configurations get the defaults.
