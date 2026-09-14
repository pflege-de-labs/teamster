# 0021. A card is claimed before it is posted

* Status: Accepted
* Date: 2026-09-14

## Context

Delivering an alert to a channel was a read, a network call and a write:
`GetActiveAlert`, then `PostMessage` to Microsoft Graph, then `UpsertActiveAlert` with the message
id that came back. Nothing tied the three together.

Two requests for the same firing alert both find no row, both post, and the second write overwrites
the first's message id. One of the two cards is then orphaned in Teams: no row names it, so nothing
will ever edit it when the alert changes or delete it when the alert resolves. It says the alert is
firing until somebody removes it by hand.

This is not a problem that more than one instance introduces. `net/http` serves concurrently, so a
single process already does it whenever Alertmanager sends the same alert twice in quick
succession, or retries a request whose response it did not see. Replicas widen the window and take
away the accidental serialisation that a single process sometimes provided. The fix is owed to the
current deployment, not the future one.

## Decision

We will claim the row before the Graph call and complete the claim afterwards, with no transaction
spanning the two.

`active_alerts` gains three columns. `claim_owner` is a token unique to one attempt, `claimed_at`
is when the claim was taken, and `posted_at` is non-`NULL` exactly when a card exists. A CHECK ties
the last to the message id — `(posted_at IS NULL) = (message_id = '')` — so no code path can record
a card with no message, or a message nothing posted. `posted_at IS NULL` is the whole state
machine: the row is a claim in flight.

Delivery then reads: claim; if a card already exists, update it and touch the row; if somebody else
holds an unposted claim, refuse with a 502 so the sender retries onto the card they are creating;
otherwise post, and complete the claim. A post that fails releases the claim immediately, so the
next attempt does not wait out the staleness cutoff.

**No transaction spans the Graph call, and there is no `SELECT ... FOR UPDATE`.** A row lock held
across a call bounded only by `graph.timeout-sec` pins a pooled connection for as long as Microsoft
takes to answer, and with a bounded pool a handful of concurrent alerts to one channel exhausts it.
It has no SQLite equivalent — the nearest thing takes the whole-database write lock. And it is
worse where it matters most: a process that dies holding a lock releases it and leaves *no record*
that a post was in flight, so the next attempt cannot tell whether a card exists. The claim row is
exactly that record, which is what lets an abandoned claim be recovered after a cutoff rather than
blocking the alert forever.

The cutoff is `max(30s, 3 × graph.timeout-sec)`, deliberately generous: a short one trades a rare
stuck claim for a common duplicate, which is the thing being fixed.

Recovery is lazy — a stale claim is taken over by the next attempt that wants it. **So there is no
reaper**, no scheduled job with a side effect, and therefore nothing here that needs leader
election. That property is worth defending when somebody later proposes a cleanup cron.

Two smaller decisions ride along, because the same reasoning produces them. `TouchActiveAlert` and
the delete that ends a resolve both match on the message id, so a resolve racing a refire cannot
stamp or delete the card that replaced the one it was working on. And the SQLite store now pins
`SetMaxOpenConns(1)` with `busy_timeout`, WAL and `foreign_keys` in the DSN: left alone,
`database/sql` opens a connection per caller against a single-writer database and turns concurrency
into `SQLITE_BUSY`. That is not a theoretical concern — the concurrency test failed exactly that
way before the limit was set.

## Consequences

Two deliveries of the same alert to the same channel now produce one card. A caller that loses the
race gets a 502 rather than a silent success, because the payload it carried may differ from the
winner's; Alertmanager retries, and the retry updates the card the winner created, so the last
state the sender sent is the state Teams ends up showing.

**What this does not close**, and the ADR should say so plainly: if a process dies between a
successful `PostMessage` and the write that records it, the card it posted is orphaned. The claim
is recovered after the cutoff and a second card is posted. Graph offers no idempotency key for a
channel message, so nothing on this side can adopt or withdraw the first one; all the store can do
is notice — `ErrClaimLost` — and log the team, channel and message id for somebody to clean up.
This converts "a duplicate on every concurrent delivery" into "a duplicate only after a crash in a
window of a few hundred milliseconds", which is the most the protocol can buy without help from
Graph.

A resolve that arrives while another instance is mid-post skips that channel and reports it
retryable, rather than deleting a claim and stranding the card about to appear. `ListActiveAlerts`
can now return rows with no card, so anything reading it must check `Posted()`.

`UpsertActiveAlert` is gone from the `Store` interface, replaced by claim, complete, release and
touch. The metrics gauge counts cards rather than rows, so a claim in flight is not reported as
something being kept up to date.

Nothing here needs Postgres, and nothing here waits for it. What Postgres will add is an advisory
lock for migrations and a pool that is not pinned to one connection; the claim protocol is written
in statements both engines run.

One limit on verification is worth recording: `graph.base-url` is configurable but the OAuth token
endpoint is not, so an end-to-end test against a mock Graph cannot authenticate. The concurrency
guarantees are covered instead by tests at two levels — eight writers racing the real SQL, and four
concurrent requests through the real HTTP stack with the winner held inside its Graph call — plus a
manual run confirming that ten concurrent deliveries whose Graph calls all fail leave no rows
behind. Making the token endpoint configurable would close that gap and is worth doing before the
two-replica rollout.
