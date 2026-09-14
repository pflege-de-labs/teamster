# 0018. Every store call takes a context

* Status: Accepted
* Date: 2026-09-14

## Context

`Store` was written against a local SQLite file, and none of its 26 methods took a
`context.Context`. That was defensible while the database was a file on the same disk: a query
either returned or the process had worse problems, and there was nothing a caller could usefully
cancel.

Two things make it indefensible now. The first is that the store is about to gain a backend
reachable over a network, where a query can block on a lock held by another replica, a connection
can hang on a machine that has gone away, and a client that hangs up should not leave work running
behind it. The second is that `AGENTS.md` already requires every command to "take a context and
return promptly once it is cancelled" — a store that discards the context makes that promise
unkeepable, because the goroutine the command is waiting on is inside a query.

There is also a mechanical forcing function. Generated query code — see the sqlc work this
precedes — emits `…Context` methods and nothing else. A `Store` without contexts would have to
call them with `context.Background()` in every method, permanently, in the one layer that most
needs to be cancellable.

The context was already available at essentially every call site: handlers have `r.Context()`,
`routing.Router.Plan` is only ever reached from a handler, `transfer.Export` and `transfer.Import`
are called from a handler and from a kong command that already has one, and `sweepSessions` took a
context before this change.

## Decision

We will pass a `context.Context` as the first argument to every `Store` method except `Close`,
which releases a handle rather than talking to one.

`WithTx` will hand the context to the function it runs — `WithTx(ctx, func(ctx, tx Store) error)` —
rather than letting that function close over the caller's. It costs two call sites today and it
means a transaction can later be given a deadline of its own without changing any of them.

Handlers derive one context from the request and use it throughout, instead of calling
`r.Context()` at each store call. Mixing the two reads as though they might differ.

Alternatives considered:

* **A wrapper that supplies `context.Background()` to generated code.** Cheaper by one large diff,
  and wrong in the one direction that matters: it makes every query uncancellable at exactly the
  moment cancellation starts to mean something, and it hides that fact behind a signature that
  looks fine.
* **Contexts on only the methods that "need" them.** The set that needs them is the set that talks
  to the database, which is all of them, and a partial interface invites a caller to assume the
  missing ones are cheap.

## Consequences

A shutdown now reaches the database: a query in flight when SIGTERM arrives is cancelled rather
than waited on, and a client that disconnects stops the work it started. That is the behaviour the
draining shutdown in [ADR 0003](0003-kong-commands-and-graceful-shutdown.md) always described and
did not previously deliver below the HTTP layer.

The interface change touched 23 files and every implementation of `Store`, including the two test
doubles. It is behaviour-preserving by construction and was driven by the compiler; the parts that
were not mechanical are three, and each is a decision rather than a translation:

* The active-alerts gauge reads through the metrics collection's own context, not the process
  context. The final collection is the one shutdown forces, by which time the process context is
  cancelled — reading through it would have made the last export, the one worth having after a
  crash, report an error instead of a number.
* `ServeCmd` returns nil rather than an error when the context is already cancelled while the store
  is opening. A signal that arrives during startup is a shutdown, not a failure to start.
* `sweepSessions` takes a one-method interface instead of `*store.SQLiteStore`. It was the last
  place the concrete type was reached for, and it had to stop being one before a second backend
  existed. This follows [ADR 0002](0002-messenger-interface.md).

What this does not do is make anything concurrent that was not concurrent before, and it does not
change what any query returns. Cancellation mid-transaction rolls back, which is what the existing
deferred rollback already guaranteed.

Nothing changes for an existing deployment: no configuration, no schema, no wire format.
