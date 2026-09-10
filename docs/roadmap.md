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

## Milestone 2 — OIDC login for the admin UI

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

## Milestone 3 — Routing visualization

A graph of routes to destinations and templates, so an operator can see which alert reaches which
channel without reading a table of label selectors. Rendered from the existing three endpoints,
with a vendored D3 build.

Includes a "which route would this alert take?" check: paste labels, and the view highlights the
matching route. That reuses `routing.SelectRoute` through a new endpoint rather than
reimplementing selector matching in the browser — the routing rules are subtle enough (priority
order, empty selectors never matching, default fallback) that a second implementation would drift.

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
| 3 | 2 OIDC login | — | IdP client registration |
| 4 | 3 Routing visualization | vendored D3 | — |
| 5 | 4 Card editor | 1.4 | decision after 1.4 |

1.3 sits after 1.4 because it is the only item waiting on someone else to grant a permission.

## Open questions

* Answered at 1.4: the vendored renderer costs 0.3 MB, taking the binary from 13.9 MB to 14.2 MB.
  Small enough that the no-CDN rule stands.
* Should the admin API accept a token for automation once OIDC lands, or is basic auth the answer
  for scripts? Decide as part of milestone 2.
* Vendored JavaScript has no update path today. A checksum file and a documented refresh procedure
  are the minimum; a `make vendor` target may be worth it.
