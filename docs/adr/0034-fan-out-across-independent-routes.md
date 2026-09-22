# 0034. Every matching root route delivers, not only the highest priority one

* Status: Accepted
* Date: 2026-09-21

## Context

[ADR 0011](0011-nested-routes.md) let one route fan out to several channels by adding children
underneath it, and made every matching child deliver rather than only the first one — "the entire
point of the feature is sending an alert to more places than one." It left one place untouched:
root selection itself. `selectRoot` still picked exactly one root — highest priority first, ties by
name, the default last — and every other root whose selector also matched was silently discarded.
That was carried forward from before nested routes existed, not decided by that ADR; it just never
came up, because two independent routes competing for the same label set was an edge case nobody
had asked for yet.

It is asked for now: two routes both naming `foo=bar` — one to the team's own channel, one to an
audit channel neither route knows about the other — should both fire. Today only the higher-priority
one does, and the other's owner has no way to know their channel went quiet short of noticing the
silence.

The claim protocol underneath this was never the obstacle. `active_alerts` keys on
`(fingerprint, team_id, channel_id)`, and every `Delivery` already names its own route, target and
template, resolved through inheritance — exactly the shape ADR 0011 built so a nested fan-out could
flow through the same `[]Delivery` the single-root case always produced. Root selection was the one
place still built to produce at most one route in that slice.

## Decision

We will let every non-default root whose selector matches deliver, the same as ADR 0011's children
already do, and for the same reason: nothing about a label selector implies exclusivity, and a
service whose whole job is fanning an alert out to more than one place should not quietly pick a
favourite when two unrelated routes both want it.

`selectRoot` becomes `selectRoots`, returning every matching non-default root in priority order
rather than the first one found. `Plan` walks each of them exactly as it always walked the one —
`collect`, inheritance, greedy suppression, `MaxDepth`, all unchanged — and concatenates the
results. **The default remains a fallback, not one more candidate.** It is tried only when the
first pass finds nothing at all; it never joins a real match, because "default" describes what
happens when nothing else claims an alert, not a low-priority route that always fires too.

`Result.RootID`/`RootName` — singular fields naming the one root `Plan` used to pick — become
`Result.Roots []string`, every matched root's id. `RootName` is dropped rather than pluralized:
nothing read the scalar field directly even before this change: every consumer already looked the
name up through a route-id map, which works exactly the same way for a slice of ids.

`internal/httpserver/webhooks.go` needed no change. `processAlert`, `deliver` and `resolveAlert`
already consumed `Result.Deliveries` as a flat slice and never inspected `RootID` — the single-root
constraint lived entirely in `internal/routing/router.go`, which is what made this a contained
change rather than a rewrite. The only other caller, `handleRoutingMatch` in
`internal/httpserver/routing_api.go`, is rewritten to answer with every matched route (`"routes"`,
plural) instead of picking one to feature as `"route"`, and `explainResult` gains a second branch
for when more than one root matched — the single-root case keeps its existing wording untouched,
since that stays by far the common case.

**Alternative considered: keep single-root selection, and ask an operator who wants both channels
notified to nest one route under the other instead.** Rejected because it inverts the relationship
a parent/child pair is supposed to express. A child route *refines* its parent — "the same alert,
but also here" — and the two are supposed to be edited together, by the same person, as one tree.
Two routes independently created by different teams for different reasons, that happen to share a
label value, are not in a parent/child relationship with each other and forcing one into that shape
to get both to fire would be modelling an accident of selector overlap as a designed hierarchy.

## Consequences

A label set can now match more than one root, and every one of them delivers. An operator who relied
on priority to mean "only this one wins" — rather than "this one wins ties, or wins when several
routes could plausibly claim the same alert but only one should" — will see routes fire that used to
lose silently. This is a behaviour change for any existing installation with two roots whose
selectors both match some label set they had not noticed overlapping; there is no migration for it,
because there is nothing to migrate — the routes were always configured that way, only the
delivery was silently narrower than the configuration said.

The routing-visualization match preview (`/admin/routing`) now highlights every matched root's path
rather than one, and its explanation names every one of them rather than describing a single
winner. `routing.match_heading` is reworded from "Which route would an alert take?" to "Which
routes would a message take?" in both catalogs.

What this does not change: nested fan-out under one matched root, which already worked exactly this
way per ADR 0011; the claim/update protocol, which was already keyed independently of how many
roots or routes produced a delivery; or the default route's role as a fallback, which stays a
fallback and never joins a real match.
