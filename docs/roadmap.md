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
    {"id": "source:webhook", "kind": "source", "label": "Incoming alerts",
     "detail": "POST /webhook/alertmanager · /webhook/universal", "x": 0, "y": 0},
    {"id": "route:abc", "kind": "route", "label": "Critical to ops",
     "selector": "severity=critical", "priority": 100, "default": false, "x": 300, "y": 0},
    {"id": "destination:def", "kind": "destination", "label": "Ops channel",
     "detail": "Platform › Alerts", "x": 600, "y": 0},
    {"id": "template:ghi", "kind": "template", "label": "Critical card", "x": 600, "y": 168}
  ],
  "links": [
    {"source": "source:webhook", "target": "route:abc"},
    {"source": "route:abc", "target": "destination:def"},
    {"source": "route:abc", "target": "template:ghi"}
  ]
}
```

The server resolves names and dangling references — a route pointing at a deleted destination is a
real state worth seeing, so it becomes a node marked missing rather than a silently absent link.
Destination details are Team and channel **names**, read through the `directoryCache` the pickers
already use, and fall back to the stored ids when Graph is unreachable: the picture is still worth
drawing without the directory.

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

Alerts travel from a webhook to a channel, so the picture is a directed acyclic graph read left to
right rather than a force-directed cloud: webhook, routes in evaluation order, then destinations
with the templates below them in the same column. A force layout arranges by repulsion, which puts
the start of the flow wherever the physics lands it — the one thing the view must not leave to
chance.

Positions are therefore computed in `buildGraph` and shipped on each node, where the Go tests can
assert the columns. D3 stays for interaction: zoom, pan and dragging a node out of an overlap,
per [ADR 0008](adr/0008-templ-tailwind-admin-ui.md)'s rule that libraries are committed rather than
fetched at page load. The full `d3.min.js` is 280 KB, against roughly 36 KB for the three modules
actually used (`d3-selection`, `d3-zoom`, `d3-drag`). Start with the full bundle because it
certainly works, and revisit if the binary size becomes uncomfortable — it is already carrying
335 KB of Adaptive Cards renderer.

Templates get their own nodes rather than being named inside the route boxes, so a template no
route uses is visible as an orphan.

Matching a route highlights the whole path it implies — webhook, route, destination, template —
and dims the rest, so the check and the graph are one view rather than two. Arrowheads carry the
direction on every edge, including the highlighted ones.

### Tests

The graph builder and the reason-returning routing function are Go, so the coverage gate covers
them: the shapes above, the columns the layout assigns, a route with a missing destination, an
unreachable directory falling back to ids, an empty configuration, and each of the four reasons.
The drawing itself is not tested; the page is asserted to render and to reference
assets that exist.

## Milestone 4 — Messages the Teams activity feed can read — done

A message whose visible content is an attached card previews as `Card` in the activity feed. An
operator scanning notifications learns nothing without opening each one, which is most of the value
of a notification.

The summary line the service already sends is hardcoded in Go — `annotations.summary`, then
`labels.alertname`, then `Alert update` (`webhooks.go`). It is not templatable, so it cannot say
what a particular deployment wants said.

### The template gains a title, and the card becomes optional

`models.Template` grows two fields beside `Body`:

* `Title` — a Go template rendering to a single line of plain text. It becomes the first line of
  the message body, which is what the feed previews.
* `Text` — an optional Go template rendering to formatted text, for a message that needs prose but
  not a card.

`Body` (the Adaptive Card JSON) becomes optional. A template with a title and text and no card
sends a plain message; a template with all three sends text with the card below it. A template with
neither a title nor a card is a validation error.

The Graph payload assembles as `body.contentType: "html"` with the title, the rendered text, and
`<attachment id="1"></attachment>` where the card goes — referencing the attachment explicitly
rather than letting Graph append it, so the card's position relative to the text is ours to decide.

### Not letting a template inject markup

`Text` renders to HTML, which makes a template author — and, through `{{ .Alert.Annotations }}`, an
alert — able to put markup into a Teams message. Rendered output passes an allowlist of the
formatting tags Teams supports, and everything else is escaped. The title is escaped outright: it
is one line of text.

### Migration

Existing templates have a body and no title. The store's migration fills `Title` with a template
reproducing today's fallback chain, so nothing changes for a deployment that never edits its
templates:

```gotemplate
{{ default .Alert.Annotations.summary (default .Alert.Labels.alertname "Alert update") }}
```

Shipped as `templates.DefaultTitle`, applied when a template has a card and no title, so no data
migration was needed — only the two columns. `default` was widened to take `any`, because a missing
key of a nil map arrives as an invalid value that a `string` parameter rejects.

`POST /api/templates/preview` returns the rendered title and text alongside the card, and the
preview pane shows the feed line above the card — the point of the milestone is the line, so the
preview has to show it.

Needs an ADR: it changes what this service sends to Teams.

## Milestone 5 — Nested routes — done

One route matches and one message is delivered. A team that wants an alert in its own channel
*and* in the platform channel has to duplicate the route and keep both selectors in step.

Alertmanager solved this with a routing tree, and operators already think in those terms, so the
model is borrowed rather than invented.

### The model

`models.Route` gains:

* `ParentID` — empty for a root route. A child is evaluated only when its parent matched, and its
  selector refines the parent's rather than replacing it.
* `Greedy` — a greedy child delivers instead of its parent; a non-greedy child delivers as well as
  its parent. This is Alertmanager's `continue` seen from the other end, and the UI says which in
  words rather than in a flag name.
* `TemplateID` and `DestinationID` become optional on a child: an unset one inherits from the
  nearest ancestor that sets it. A child that inherits both and only refines the selector is
  pointless, and validation says so.

The tree is bounded: a parent chain may not cycle, and depth is capped. Both are checked on write,
because a cycle found at delivery time is an alert that never arrives.

### Selection becomes a plan, not a route

`routing.Match` returns one route today. It becomes:

```go
type Delivery struct {
    RouteID, DestinationID, TemplateID string
    Reason                             Reason
}

func (r *Router) Plan(labels map[string]string) ([]Delivery, error)
```

Children are evaluated in priority order. A greedy match drops its ancestor's delivery from the
plan; a non-greedy one leaves it in. `SelectRoute` retired with its last caller.

**Shipped differently:** every matching child delivers, rather than the first one winning as
sketched here. With a single flag the inverse default is the coherent one — the feature exists to
send an alert to more places than one — and a sibling-suppressing flag can be added later without
taking fan-out away. `Plan` returns a `Result` carrying the reason and the matched root beside the
deliveries, because the explanation of a fan-out has to name where it started. See
[ADR 0011](adr/0011-nested-routes.md).

### State tracking has to fan out too

`active_alerts` is keyed by fingerprint alone and holds one message id. With fan-out an alert has a
card in several channels, so the key becomes `(fingerprint, team_id, channel_id)` and resolving an
alert updates every card it posted. That is a real migration of an existing table, not an additive
column, and it carries the same legacy-schema guard the `DATETIME` fix introduced.

Delivery is best effort per destination: one channel failing must not cost the others their update.
Because each delivery records its message id, an Alertmanager retry after a partial failure updates
the cards that made it rather than duplicating them — so a partial failure can still answer `502`
and let the sender retry.

### The UI

The route form gains a parent selector and the greedy choice, worded as "instead of" versus "as
well as". The route list becomes a tree, indented by depth, showing inherited destinations and
templates in a lighter style so inherited and set are distinguishable at a glance.

Needs an ADR: routing stops being "one alert, one message".

## Milestone 6 — Routing visualization, second pass — done

Milestone 5 changes what there is to draw, and the current picture has a flaw worth fixing at the
same time: templates are nodes, which makes an edge from a route to a template mean something
different from an edge to a destination. That is two graphs drawn on top of each other.

* Templates stop being nodes. A route node carries its template as a label, marked as inherited
  when it comes from an ancestor, which is also how milestone 5 wants to display inheritance.
* Route nodes nest: webhook → root routes → child routes → destinations. Parent-to-child edges
  carry the greedy choice, and a non-greedy child keeps its parent's edge to its own destination,
  so the fan-out is visible as two paths rather than described in a tooltip.
* The graph is a strict DAG again, with one kind of edge meaning one thing: "an alert can go this
  way".
* A second, small graph pairs templates with the routes that use them. It is where a template no
  route references shows up — the orphan case the template column was carrying.
* The match highlight follows every path a label set takes, because a match can now end in more
  than one channel.

Depends on milestone 5. No ADR: it is the same page drawing a changed model.

## Milestone 7 — Fine-grained permissions — done

Today an authenticated session can do anything. The roles we want are `admin`, `editor` and
`viewer`, with an admin able to say which Teams and channels an editor — or a group of editors —
may deliver to, and able to set which teams and channels are visible at all.

### Where the roles come from

The claim that already carries membership carries them, matched by name: a provider role called
`admin`, `editor` or `viewer` is that role here, with nothing in between to configure.
`auth.default-role` covers a user the claim names no role for, and leaving it empty means they sign
in with no access and are told to ask an administrator for one.

### Why not write the checks by hand

Per-object grants with group inheritance and a visibility overlay is a policy model, and hand-rolled
versions of it grow into a small, untested authorization engine scattered across handlers. Use one
that already exists.

### Cedar, embedded

**[`cedar-policy/cedar-go`](https://github.com/cedar-policy/cedar-go).** Cedar is a policy language
with an authorizer that runs in-process: policies are `permit`/`forbid` over a principal, an action
and a resource, with conditions and an entity graph that carries group membership as parent
relationships. It is a library, not a service, so it costs this project no second process and no
second database — which is what the first constraint in this file demands.

Policies read close to how an admin would say the rule out loud:

```cedar
permit (
  principal in Group::"payments-editors",
  action in [Action::"deliver", Action::"edit"],
  resource in Team::"platform"
);

forbid (principal, action == Action::"edit", resource)
unless { principal in Group::"editors" };
```

Entities come from what this service already knows: the session gives the principal and the claim
values give its groups, while teams, channels and destinations are the resources — with a channel's
team as its parent, so a grant on a Team reaches the channels in it.

[OpenFGA](https://openfga.dev/) was the first candidate and is the rejected alternative: a
Zanzibar-style model fits the shape of the problem just as well, but it is a service whose storage
engine would have to run alongside the binary, or be embedded against a database it may not support.
Cedar answers that by being a library in the first place.

Open for the ADR: where policies live — embedded defaults derived from the three roles, an operator
file in the XDG config directory, a table in SQLite edited through the admin UI, or some combination
— and how much of Cedar's schema validation the Go implementation offers, which decides whether a
bad policy is caught on write or only at evaluation. The library's API surface gets read before any
of that is promised.

### How it shipped

In two parts. First the roles: claim values map to `admin`, `editor` and `viewer` by name,
`internal/authz` evaluates the Cedar policies in-process, one middleware enforces them, and the UI
hides what a role may not do. Then the scoping: a `grants` row ties a role to a Team or one channel
of it, three scoped actions decide against it, and the pickers, the lists and the writes all go
through it. A role no grant names stays unrestricted, so nothing narrows until an admin says so.
[ADR 0012](adr/0012-role-based-authorization.md) records both.

The addition was entities and three policies rather than a rewrite, which is what choosing a policy
engine was supposed to buy.

### Enforcement

Authorization is checked in the admin API handlers, which is the only way the UI reaches the
service, so the UI cannot be the place a permission is enforced. The pickers list only visible
teams and channels, a route may only point at a destination the editor may deliver to, and a viewer
sees the lists with every mutating control absent. Delivery is unaffected: it is a machine path
with no user attached.

A denied check is a `403` that names what was refused, not a `404`: an editor who cannot deliver to
a channel needs to be told that, rather than left to conclude the channel does not exist.

Needs an ADR, and it supersedes part of [ADR 0009](adr/0009-admin-authentication.md): that one says
membership grants access, and this one says membership grants a role.

## Milestone 8 — Import and export of configuration — done

There is no way to move a configuration between installs or to back one up other than copying the
SQLite file, which carries sessions and alert state along with it.

`GET /api/config/export` returns a bundle — templates, destinations, routes, and the permission
grants once milestone 7 exists — with a schema version and an export timestamp. Secrets are not in
it: no webhook token, no client secret, no admin password. Sessions, login flows and active alerts
are runtime state and are not in it either.

`POST /api/config/import` takes the same document with a mode:

* `merge` upserts by id and leaves anything absent from the bundle alone.
* `replace` makes the install match the bundle, deleting what the bundle does not mention.

Both validate the whole bundle first — dangling destination and template references, cycles in the
route tree, unknown schema version — and apply it in one transaction, so a rejected import changes
nothing. A dry run returns the diff it would apply without applying it.

Ids are preserved, so a bundle re-imported into the install it came from is a no-op. Team and
channel ids are tenant-specific, though: exporting from one tenant and importing into another
leaves destinations pointing at ids that do not resolve. The bundle therefore carries the Team and
channel *names* beside the ids as a hint, and the import reports which destinations no longer
resolve rather than silently keeping them.

This is also the first thing the CLI does beyond `serve`: `teamster export` and `teamster import`
are kong commands over the same code the endpoints use, which is what the command structure in
[ADR 0003](adr/0003-kong-commands-and-shutdown.md) was built for.

## Milestone 9 — Card editor — done, as something smaller

The question this was left open for has an answer: with the preview in hand, the JSON is not the
awkward part. The awkward parts were an empty textarea with no hint of what an alert offers, typing
a FactSet from memory, clicking Preview after every change, and a quotation mark in an alert summary
breaking a card invisibly.

So: no visual designer and no form editor — a template is a Go template that renders *into* a card,
and `{{ if eq .Alert.Status "firing" }}` is not JSON, so a WYSIWYG editor would either refuse half
the templates this service runs or discard their templating on save.

What shipped instead: a palette of the elements our own templates use, inserted at the cursor with
the comma when one is needed; a starter card so a new template begins as a working example; and
preview as you type, debounced, with the render still on the server. The fragments live in
`internal/cards` where a test renders every one of them against a sample alert and an empty one,
because a palette that inserts a card the renderer rejects is worse than no palette.

See [ADR 0014](adr/0014-card-editor.md). A visual designer is still not built; if it is what people
ask for, that ADR is where the reasons to supersede are written down.

## Milestone 10 — A localizable UI — done

Every string the admin UI shows is written into the templ component that shows it. Changing the
wording of one sentence means finding the component that holds it, and there is no way to offer the
UI in another language at all — the strings and the markup are the same file.

Two problems, one fix: get the text out of the templates and into a catalog.

### Where the strings live

A message catalog per language, keyed by a short identifier, with the English catalog as the
source. Components ask for a key and never hold a sentence. Tooling extracts the keys and reports
the ones a catalog is missing, so a translator works from a list rather than by reading the markup.

**Shipped as:** `x/text/language` for negotiation and plain JSON catalogs read by a twenty-line
lookup. `x/text/message` with `gotext` was not used — its extraction pipeline wants Go source as the
target and a generated catalog as the output, which fits a program printing to a terminal better than
templ components, and JSON files an operator can edit are the artefact we wanted. See
[ADR 0015](adr/0015-localizable-ui.md).

### Reaching the printer from a component

templ components take a `context.Context`, so the negotiated printer travels in it and a component
calls a small `T(ctx, key, args...)` helper. Passing it through `Page` instead would mean every
component that renders a string needs it in its parameters, which is the sort of change that gets
half-done.

### Which language

`Accept-Language`, matched against the languages this build actually carries with
`language.NewMatcher`, with a configured default for when the header says nothing useful. A
per-session preference — an explicit choice that outlives one request — is the obvious follow-up
once there is more than one language to choose.

### Editable without a release

Shipped in the first pass after all: `ui.locale-dir` is read after the embedded catalogs and wins
entry by entry, so an operator can retune a sentence — or add a language this build has never
carried — without waiting for a release.

### What is not localized

Webhook payloads, API errors and log lines stay English: they are read by machines, and by whoever
is reading a log at three in the morning, and a translated error is harder to search for.

Alert templates are already the operator's own text, in whatever language they wrote them. The one
string this service still supplies for an alert — `templates.DefaultTitle`, the fallback summary
line — is a template and can be overridden per template, so it needs nothing new here.

### Testing the catalogs

German ships beside English, so the second language is real rather than theoretical. A test that
every catalog carries every key the source catalog does and keeps every placeholder; a table over
`Accept-Language` headers and the language each should select, including a header naming a language
this build does not carry; and a missing key falling back to the source string rather than rendering
an empty element or panicking.

Needs an ADR: it changes how every component in the UI is written.

## Milestone 11 — Metrics worth alerting on

The service that routes alerts produces none of its own. Whether deliveries are failing, how long
Graph is taking, how many alerts are in flight — none of it leaves the process except as log lines,
which nobody aggregates until the day they need them.

### What to measure

Counters and histograms about the work, not about the runtime:

* deliveries by outcome — posted, updated, refused by Graph — and by route, because "delivery is
  broken" and "one channel is broken" want different responses
* webhook receipts by source and status, including the ones rejected for a bad token
* Graph call duration and failures, which is the dependency most likely to be the problem
* rendering failures by template, which today surface only as a `502` to Alertmanager
* active alerts currently tracked, so a leak in the state table is visible before it is a problem

The Go runtime collector comes free with either client library and is worth having, but it is not
the point.

### Prometheus and OTEL, without choosing

`GET /metrics` in the Prometheus text format is what most deployments will scrape, and the rest
export OTLP to a collector. Both are wanted. The way not to write everything twice is to instrument
once against OpenTelemetry's metric API and attach two readers: the Prometheus exporter from
`go.opentelemetry.io/otel/exporters/prometheus` serving `/metrics`, and an OTLP exporter when one is
configured. That keeps a single set of instruments in the code and makes the choice a matter of
configuration.

The alternative — `prometheus/client_golang` directly, with OTLP bridged from it — is fewer
dependencies for the common case and more work for the other one. The ADR picks, against the actual
size the two pull in.

### Where it is exposed

`/metrics` is unauthenticated like the probes, or behind the same basic auth as `/api` — an
operator's answer depends on whether their network already isolates it. Configuration decides, and
the default should be off, because the metrics name templates, routes and channels.

Needs an ADR: it adds an export surface and a dependency that will be in every build.

## Milestone 12 — More than one instance, more than SQLite

SQLite takes one writer, and the store runs its migrations at startup. Two replicas would race on
both, so a deployment is one pod, `ReadWriteOnce`, and a restart is a gap in alert delivery.

For most installations that is fine. For one where an alert that does not arrive is a real problem,
it is not.

### What actually blocks it

* **The store.** `store.Store` is an interface and `httpserver.NewServer` takes it, so a second
  implementation is a new type and a driver setting rather than a refactor. What is SQLite-shaped is
  the migration code — `PRAGMA table_info`, and the `active_alerts` rebuild that exists because
  SQLite cannot alter a primary key — the `DATETIME` declared-type guard, and `?` placeholders.
* **Migrations on start.** Two instances starting together must not both migrate. An advisory lock
  in the database, or a migration that runs as its own step, rather than every process racing.
* **Alert state.** `active_alerts` is how a repeated `firing` finds its card instead of posting a
  second one. Two instances handling the same alert concurrently need that to be atomic — a unique
  key and an upsert that returns what it did, rather than the read-then-write there is today.
* **The session and login-flow tables**, which are already shared state and would simply work.
* **The directory cache**, which is per-process and would merely be warmed twice.

Postgres is the obvious second backend: it is what the deployments that want two replicas already
run.

### What it is not

Not a queue, not leader election, not sharding. Two or three instances behind a service, each able
to take any request, sharing one database. Anything more is a different service.

Needs an ADR, and probably a second one for how migrations are sequenced.

## Milestone 13 — Alerts in a person's chat

An alert reaches a channel. Somebody on call at three in the morning is not reading a channel; they
want the thing in front of them. The ask is an opt-in: a person says "send my alerts to me" and they
arrive as a chat rather than only in Teams they happen to watch.

### Why this is not another endpoint on the Graph client

Teamster authenticates to Microsoft with **client credentials** — an application identity, no user.
That is what lets it post to a channel unattended, and it is precisely what cannot post to a chat:
sending a `chatMessage` has required a **delegated** permission, a token obtained on behalf of a
signed-in user. Application permissions cover the migration path (`Teamwork.Migrate.All`, for
backfilling history in import mode) and not this.

So the question is not which endpoint to call. It is **whose identity the message is sent under**,
and each answer is a different integration.

### Three routes, and what each costs

**A. Delegated token per user.** A second authorization-code flow, against Microsoft this time, with
each person consenting once; Teamster keeps a refresh token and uses it when an alert fires.

The message is then sent **as that person**. An alert delivered this way is not a message from
Teamster to Alice — it is a message Alice appears to have written, to herself. That is workable as a
notification and strange as a conversation, and it is the thing to decide before any of the rest
matters.

It also changes what the database is. Today it holds no credentials at all, which is why a
configuration bundle can be kept in a repository ([ADR 0013](adr/0013-configuration-transfer.md)).
Refresh tokens are credentials: they need encrypting at rest, they must never reach an export, and
losing the database becomes an incident rather than an inconvenience. Consent is also revocable and
conditional access can invalidate a token at any time, so delivery has to degrade when a token stops
working rather than dropping the alert.

**B. A Teams app with a bot.** Proactive messaging through the Bot Framework: the message comes from
a bot, which is what a person expects an alert to look like. The bot must be installed for each
recipient, and Teamster stores a conversation reference per person rather than a credential.

This is the right shape and the largest one: a Teams app package, a bot registration, an endpoint
Microsoft can call, and an install story. It is a second service-facing integration, not an addition
to the Graph client.

**C. An activity feed notification.** `POST /users/{id}/teamwork/sendActivityNotification` with the
`TeamsActivity.Send` application permission — the credential model Teamster already has. It puts an
entry in the person's Activity feed, linking somewhere, rather than a message in a chat. It needs a
Teams app registered and installed for the user, but no per-user token and no per-user secret.

Cheapest by a distance, and honestly less than what was asked for: a notification, not a message.

### One app registration or two

A single Entra registration can hold both application and delegated permissions — they are separate
lists, and the token type decides which apply. It would work.

Two registrations is the better answer anyway, and is what this milestone proposes if route A is
taken: the channel-posting identity keeps its narrow application permissions and its existing
secret, and the chat identity is consented to separately, revoked separately, and absent entirely
from a deployment that does not want the feature. A permission that only some installations use
should not be on the credential every installation runs.

### What it would look like in the product

Opt-in per person, in the admin UI, under their own account rather than something an admin sets for
them: consent is theirs to give. A route gains "and to whoever asked for it", which means the
delivery plan grows a second kind of destination — `routing.Delivery` currently resolves to a Team
and a channel, and a person is neither.

Needs an ADR, and the decision it records is A, B or C rather than the details of any of them.

**Prerequisite:** whichever route, a Teams app registration in the tenant, and for A or B the
agreement that Teamster may hold something per person — a token or a conversation reference.

### Worth checking before starting

The permissions around chat messages have moved more than once: application-permission channel
posting was gated behind a Microsoft approval process for a while, and resource-specific consent
(`ChatMessage.Send.Chat`) added a fourth shape for apps installed in a chat. The *Send chatMessage*
permissions table is the first thing to read when this milestone starts, not the last.

## Sequencing

| Order | Item | Depends on | Blocked by |
| --- | --- | --- | --- |
| — | 1.1 Selectable ids | — | done |
| — | 1.2 Fuller lists | — | done |
| — | 1.4 Template preview | — | done |
| — | 1.3 Teams picker | — | done |
| — | 2 OIDC login | — | done |
| — | 3 Routing visualization | — | done |
| — | 4 Activity feed messages | — | done |
| — | 5 Nested routes | — | done |
| — | 6 Visualization, second pass | 5 | done |
| — | 7 Fine-grained permissions | 2 | done |
| — | 8 Import and export | 7 for permissions | done |
| — | 9 Card editor | 1.4 | done |
| — | 10 Localizable UI | — | done |
| 8 | 11 Metrics | — | ADR on OTEL versus Prometheus directly |
| 9 | 12 More than one instance | 11 helps | ADR, and a second backend |
| 10 | 13 Alerts in a person's chat | — | ADR choosing the route; a Teams app registration |

1.3 sat after 1.4 because it was the only item waiting on someone else to grant a permission.

Milestone 4 goes first because it is small, changes no schema beyond two template columns, and
fixes something an operator hits on every single alert. Milestone 5 is the large one and 6 follows
it immediately, because shipping a picture that disagrees with routing is worse than shipping
neither. 8 waits on 7 only for the permission part of the bundle; the rest of it could be pulled
forward if a migration is needed sooner.

13 sits last because it is the only item that would make Teamster hold something per person — a
token or a conversation reference — and that is worth wanting badly before taking it on. The
activity-feed route (C) is the exception: it needs no such thing and could be pulled forward on its
own if a notification is enough.

11 and 12 sit after the feature work because both are about running the service rather than using
it, and 12 in particular is worth doing when somebody actually needs a second replica: the single
instance is a real constraint but not yet a real problem. 11 comes first of the two because knowing
what the service is doing is most of what makes an HA deployment reviewable.

10 sits last of the feature work because 7 and 9 both add screens, and extracting strings from a UI
that is still growing means doing it twice. The counter-argument is real though: everything built
before it adds
more strings to extract later, so if a second language is actually wanted, pull it forward ahead of
7 and let the new screens be written against the catalog from the start.

## Open questions

* Answered at 1.4: the vendored renderer costs 0.3 MB, taking the binary from 13.9 MB to 14.2 MB.
  Small enough that the no-CDN rule stands.
* Should the admin API accept a token for automation once OIDC lands, or is basic auth the answer
  for scripts? Decide as part of milestone 2.
* Answered before milestone 7 started: authorization uses `cedar-policy/cedar-go`, which is a
  library rather than a service, so the single-binary promise holds without embedding somebody
  else's server. What stays open is where the policies are stored and edited.
* Answered at milestone 5: a route's delivery is its own, resolved by inheriting the nearest
  ancestor's destination and template. Suppression is likewise local — a greedy child drops its
  parent's delivery, not its grandparent's.
* Should a message that carries only text still be updated in place when an alert resolves, or is
  editing a plain message in Teams confusing in a way editing a card is not?
* Should teamster grow a second store implementation? The Helm chart
  ([ADR 0016](adr/0016-helm-chart.md)) deploys one pod because SQLite takes one writer, and it
  rejects `database.driver=postgres` rather than pretending. A remote database would buy several
  replicas and a backup story the cluster already has, at the cost of a schema, a migration path
  and a second dialect in `internal/store`. That is now milestone 12, which is where the answer
  belongs; what is still open is whether anybody wants it enough to build it.
* Which identity should an alert in a chat come from — the person receiving it, or a bot? Route A
  makes the alert look like something the recipient wrote to themselves, which is the cheapest to
  build and the strangest to read. Decide before milestone 13 starts.
* Vendored JavaScript has no update path today. A checksum file and a documented refresh procedure
  are the minimum; a `make vendor` target may be worth it.
