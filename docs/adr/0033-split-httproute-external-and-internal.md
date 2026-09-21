# 0033. Split the chart's HTTPRoute into external and internal

* Status: Accepted
* Date: 2026-09-21

## Context

`charts/teamster/templates/httproute.yaml` rendered one `HTTPRoute`, on one set of `parentRefs`
and `hostnames`, carrying whatever `rules` the operator supplied. In practice that meant the
webhook and bot endpoints — `/webhook/alertmanager`, `/webhook/universal`, `/teamsv2/*`,
`/bot/messages`, each authenticating itself with a shared token, a URL token, or Microsoft's own
signature — and the admin UI and API — gated by a session or basic auth — were published through
the same Gateway listener and the same hostname. A cluster that wants the webhooks reachable from
the internet but the admin interface reachable only through a private Gateway listener, or a
VPN-only hostname, could not express that: it was one route or none.

`internal/httpserver/server.go` already draws this line at the application layer — the
self-authenticating paths are registered on the plain `mux`, everything else sits behind
`requireSession`/`apiAuth` (see ADR 0026, ADR 0030). The chart's single route ignored it.

## Decision

We will render up to two `HTTPRoute`s, `httpRoute.external` and `httpRoute.internal`, each with
its own `enabled`, `parentRefs`, `hostnames`, `matches`, `filters` and `timeouts`.

`httpRoute.external.matches` defaults to the application's own self-authenticating paths —
`/webhook/alertmanager`, `/webhook/universal`, a `/teamsv2` prefix, and `/bot/messages`.
`/bot/messages` is matched unconditionally even though the chart has no dedicated "is the bot
configured" flag (`config.settings.bot` is a raw passthrough, like `graph`): matching a path the
application has not registered is harmless, since the application decides what happens next — the
Gateway rule only decides which route the request arrives through.

`httpRoute.internal.matches` defaults to a single `PathPrefix: /` — everything the external
matches do not name.

**`httpRoute.internal` is off by default, and while it is, `httpRoute.external` folds in
`httpRoute.internal`'s rules too**, so the rendered route reaches everything, exactly as the
chart's original single-`enabled` flag did. Enabling `httpRoute.internal` splits the two apart
onto their own `parentRefs`/hostnames. Gateway API's own rule-matching precedence (exact over
prefix, longer prefix over shorter, regardless of declaration order) is what makes combining the
four specific matches with the catch-all on one route safe in the fallback case: the specific
paths still win.

The fallback runs **one way only**. `httpRoute.internal.enabled=true` with `httpRoute.external`
left off does not expose the webhooks — that would change what a deployment publishes based on a
flag named for the opposite purpose, and a deployment that wants the webhooks reachable already
has the tool for that: turn `httpRoute.external` on.

This is a **breaking change** to the values schema: `httpRoute.enabled`/`.parentRefs`/`.hostnames`/
`.rules` become `httpRoute.external.*` and `httpRoute.internal.*`. Accepted because the chart is
pre-1.0 (`0.2.0`) and this repository has taken breaking chart changes before when they made the
shape cleaner (the Postgres backend, "chart deploys either shape").

Ingress is deliberately left unsplit. It keeps its single-resource, all-paths shape; an operator
who needs the split uses Gateway API.

## Consequences

* A deployment upgrading past this chart version must rename its `httpRoute:` values —
  `httpRoute.enabled` → `httpRoute.external.enabled`, and `httpRoute.hostnames`/`.parentRefs` move
  under `.external`. The chart README documents the new keys; there is no migration shim, matching
  how the Postgres-backend split was handled.
* The `metadata.name` of the rendered `HTTPRoute` changes from `{{ fullname }}` to
  `{{ fullname }}-external` (and, when enabled, `{{ fullname }}-internal`) — the bare name would
  otherwise collide the moment two resources render. A Gateway API controller or DNS record that
  referenced the old bare name needs updating.
* A deployment that wants the split has two hostnames (or two listeners) to manage instead of one,
  and two `HTTPRoute`s to reason about. That is the cost of the isolation this ADR exists to make
  possible.
* `scripts/check-chart-render.sh` gained assertions for both shapes: the fallback (one route,
  everything reachable) and the split (two routes, the catch-all confined to internal). Verified
  by mutation — removing the fallback rule, and leaking it into a split render — that both
  assertions actually fail when the property they check is broken.
