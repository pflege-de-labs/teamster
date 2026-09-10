# Roadmap

Planned work, in the order we intend to build it. Each feature is designed before it is
implemented: a short design note in its pull request, an [ADR](adr/) when it changes how components
are structured, and one feature per pull request.

This file records intent, not commitments. It is edited as features land or plans change.

## Constraints every milestone works within

* The service ships as one static binary with its assets embedded. No CDN at runtime, no separate
  frontend deployment.
* No runtime dependency on a CDN. A generator whose output is committed is allowed — templ and
  Tailwind work that way ([ADR 0008](adr/0008-templ-tailwind-admin-ui.md)) — but a library the
  browser fetches at page load is not. Vendored files go under
  `internal/httpserver/web/vendor/`, at the cost of manual updates Renovate cannot see.
* Logic lives in Go where there is a choice, because that is what the 75% coverage gate measures.
  The browser gets rendering, not decisions.
* The admin API stays the only way the UI reaches the service, so anything the UI can do is
  scriptable.

## Milestone 1 — Admin UX — done

The current UI works but makes the operator carry identifiers by hand. Everything here is
low-risk, visible, and needs no new authentication model.

### 1.1 Selectable destinations and templates — done

Delivered by the move to server rendering: the handler already holds both lists, so the route form
uses `select` elements and the route list shows resolved names.

### 1.2 Fuller lists — done

Routes show their selector, priority and default flag; destinations their team and channel;
templates their size and last update, and each list has an empty state that says what to create
first. Each row offers Edit, which reloads `/admin` with that record selected and its form
prefilled. Sorting was never open: the store orders templates and destinations by name and routes
by priority then name.

### 1.3 Teams and channel picker — done

Destinations are created by pasting a Team ID and a Channel ID out of the Teams client. Add
`GET /api/graph/teams` and `GET /api/graph/teams/{id}/channels`, backed by new methods on the Graph
client, and turn both destination fields into pickers.

`Team.ReadBasic.All` and `Channel.ReadBasic.All` are granted with tenant admin consent. A
deployment without them still works: the fields ship as text inputs and the picker simply never
appears.

Responses are cached in memory with a short TTL; a tenant's team list is stable and the Graph
throttles.

### 1.4 Template preview — done

Render a template against a sample alert and show the resulting card.

The template body is Go template syntax, so the browser cannot render it alone. Add
`POST /api/templates/preview`, which takes a template body and an alert payload and returns the
rendered Adaptive Card JSON through the existing `templates.Render`. The browser then draws it with
the vendored Adaptive Cards renderer. The samples in [samples/](../samples) become the built-in
payload choices, and a rendering error is reported in the response rather than swallowed.

This keeps templating in one implementation, which matters because a preview that disagrees with
what Teams receives is worse than no preview.

## Milestone 2 — OIDC login for the admin UI — done

Basic auth means one shared password with no attribution: every change in the audit trail is
`admin`. Replace it with an OIDC authorization code flow with PKCE against any provider that
publishes discovery metadata.

* Configuration: issuer URL, client id, client secret, redirect URL, and the claim that carries
  group or role membership. Discovery supplies the endpoints.
* A signed, `HttpOnly`, `SameSite=Lax` session cookie holds the session. Sessions are stored in
  SQLite alongside the other state.
* Access is granted by a claim value, so an operator group in the IdP controls who gets in.
* Basic auth stays available and is documented as the bootstrap path for a deployment with no IdP,
  and for automation against the admin API.
* The webhook endpoints keep their shared token. They are machine-to-machine and Alertmanager
  cannot do an authorization code flow.

Needs an ADR: it changes the trust model and adds session state to a service that had none.

**Prerequisite:** a client registration in the IdP with the redirect URL, and a secret to store.

## Milestone 3 — Routing visualization — done

A graph of routes to destinations and templates, so an operator can see which alert reaches which
channel without reading a table of label selectors, plus a check that answers "which route would
this alert take?".

### The page

`/admin/routing`, behind a session like the rest of the admin UI, linked from the header. Its own
page rather than a fourth panel on `/admin`, which is already long.

### Where the data comes from

`GET /api/routing/graph` returns the graph ready to draw, rather than the browser stitching three
endpoints together:

```json
{
  "nodes": [
    {"id": "route:abc", "kind": "route", "label": "Critical to ops",
     "selector": "severity=critical", "priority": 100, "default": false},
    {"id": "destination:def", "kind": "destination", "label": "Ops channel",
     "detail": "team … · channel …"},
    {"id": "template:ghi", "kind": "template", "label": "Critical card"}
  ],
  "links": [
    {"source": "route:abc", "target": "destination:def"},
    {"source": "route:abc", "target": "template:ghi"}
  ]
}
```

The server resolves names and dangling references — a route pointing at a deleted destination is a
real state worth seeing, so it becomes a node marked missing rather than a silently absent link.

### The route check

`POST /api/routing/match` takes label pairs and answers with the route that would win and why:

```json
{"labels": {"severity": "critical"}}
→ {"route": {"id": "abc", "name": "Critical to ops"}, "reason": "selector"}
```

`reason` is one of `selector`, `default`, `none` or `no-routes`. Getting that from
`SelectRoute` alone is not possible: it returns a route without saying whether the selector matched
or the default caught it. So `internal/routing` gains an exported function that returns the route
and the reason, and `SelectRoute` becomes a thin wrapper over it. The rules stay in one place —
priority order, empty selectors never matching, default fallback — because a second implementation
in the browser would drift from delivery.

The form takes `key=value` lines, which is how operators read Alertmanager labels, and posts them
as JSON.

### Drawing it

A vendored D3 force layout, per [ADR 0008](adr/0008-templ-tailwind-admin-ui.md)'s rule that
libraries are committed rather than fetched at page load. The full `d3.min.js` is 280 KB, against
roughly 36 KB for the four modules actually used (`d3-force`, `d3-selection`, `d3-zoom`,
`d3-drag`). Start with the full bundle because it certainly works, and revisit if the binary size
becomes uncomfortable — it is already carrying 335 KB of Adaptive Cards renderer.

Matching a route highlights its node and dims the rest, so the check and the graph are one view
rather than two.

### Tests

The graph builder and the reason-returning routing function are Go, so the coverage gate covers
them: the shapes above, a route with a missing destination, an empty configuration, and each of the
four reasons. The drawing itself is not tested; the page is asserted to render and to reference
assets that exist.

## Milestone 4 — Card editor

Deliberately last. Once the preview from 1.4 exists we will know whether editing JSON beside a live
preview is already enough.

If an editor is still wanted, the options are Microsoft's Adaptive Cards Designer, which is large
and does not round-trip Go template syntax cleanly, or a form-based editor over the card elements
this service actually uses. The decision gets its own ADR when we get there.

## Sequencing

| Order | Item | Depends on | Blocked by |
| --- | --- | --- | --- |
| — | 1.1 Selectable ids | — | done |
| — | 1.2 Fuller lists | — | done |
| — | 1.4 Template preview | — | done |
| — | 1.3 Teams picker | — | done |
| — | 2 OIDC login | — | done |
| — | 3 Routing visualization | — | done |
| 5 | 4 Card editor | 1.4 | decision after 1.4 |

1.3 sits after 1.4 because it is the only item waiting on someone else to grant a permission.

## Open questions

* Answered at 1.4: the vendored renderer costs 0.3 MB, taking the binary from 13.9 MB to 14.2 MB.
  Small enough that the no-CDN rule stands.
* Should the admin API accept a token for automation once OIDC lands, or is basic auth the answer
  for scripts? Decide as part of milestone 2.
* Vendored JavaScript has no update path today. A checksum file and a documented refresh procedure
  are the minimum; a `make vendor` target may be worth it.
