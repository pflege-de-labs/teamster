# 0073. Authorize from a versioned policy snapshot

* Status: Accepted
* Date: 2026-10-01

## Context

Authorization evaluates the embedded `policies.cedar` against an entity graph of the three roles
([ADR 0012](0012-role-based-authorization.md)). The graph is the same for every request. The next
steps of milestone 25 add state that lives in the store:

* local groups, which become entities and parents;
* per-record permissions, which become Cedar policies (see the [roadmap](../roadmap.md)).

Two things follow from that:

* **Rebuilding the policy set and graph on every request is wasteful.** Most requests change
  nothing.
* **Caching them for a while is wrong.** Delivery grants have always been read on every request,
  so a revoked permission takes effect at once. A cache with a TTL would let it linger. With
  several Postgres replicas, a change made through one replica would linger in the others.

Those permissions also need finer actions than `view` and `edit`: read, update, delete, share and
pass on ownership. The role policies have to keep their meaning when those actions arrive.

## Decision

We will build an immutable `authz.Authorizer` snapshot from the embedded policies plus a model read
from the store. The snapshot is versioned by a single counter row, `authz_generation`.

* **The engine.** `authz.Engine` holds the current snapshot. `Engine.Authorizer(ctx)` reads the
  generation, one indexed single-row select. When the generation matches the snapshot, it returns
  the snapshot. Otherwise it loads the model, builds a new snapshot once under a mutex, and swaps
  it in atomically.
* **Bumping.** Every write that changes the model must bump the generation inside its own
  transaction. Groups and permissions, in later steps, do. Replicas then pick up a change on their
  next request, with no pub/sub and no expiry.
* **One snapshot per request.** `httpserver.authorize` resolves the snapshot once per request and
  puts it in the context. Every check in the request, the middleware's and the handlers', answers
  from the same generation.
* **Failure.** If the generation cannot be read, the request fails closed with `503`.
* **Entities.** A request's own entities, its principal and the resources a scoped check walks, sit
  in an overlay `EntityGetter` over the snapshot's map. The snapshot's map is never copied.
* **Actions nest.** Actions become a hierarchy through Cedar action entities:

  | Action | Part of |
  | --- | --- |
  | `read` | `view` and `own` |
  | `create` | `edit` |
  | `update`, `delete`, `attach` | `edit` and `own` |
  | `share`, `transfer` | `own` only |

  The viewer policy changes from `action == Action::"view"` to `action in Action::"view"`. Editors
  and viewers therefore keep exactly what they had under the new names, and only admins hold
  `share` and `transfer` until ownership exists.

In this step the model is empty, so behaviour does not change. Groups and permissions fill it in
later steps.

Alternatives considered:

* **A TTL cache.** This is the lag ADR 0012's grants deliberately avoid.
* **Change notifications between replicas** (Postgres `LISTEN`, NATS). They add a moving part and a
  failure mode: a missed notification means a stale snapshot. The counter costs one read per
  request and cannot be missed.
* **Rebuilding per request.** It is correct, but every request pays for parsing policies and
  walking groups.

## Consequences

* Each admin and API request reads one more row. Webhooks do not read it yet.
* A store that cannot be read now refuses admin requests with `503`, where before only the
  handlers that touched the store failed.
* A model change that forgets to bump the generation is invisible to other replicas until something
  else bumps it. The store's conformance tests cover the bump, and each model change will test it.
* `authz_generation` is new: SQLite `0025`, Postgres `0022`. The previous release ignores it.
