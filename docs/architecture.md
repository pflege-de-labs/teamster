# Architecture

Teamster is a single Go binary that accepts alert webhooks, picks a Teams channel and an
Adaptive Card template per alert, and posts or updates the card through the Microsoft Graph API.
State lives in a SQL store: a local SQLite file by default, or a Postgres server for a deployment
that runs more than one instance. SQLite is the option with no other runtime dependency.

## Components

| Package | Responsibility |
| --- | --- |
| `cmd/teamster` | Entry point. Installs the signal handler and hands the resulting context to the command tree. |
| `internal/cli` | kong command tree and its wiring. `ServeCmd` is the default command: it opens the store, constructs the Graph client, serves HTTP and shuts down on cancellation. `completion` writes a shell completion script generated from the same model. |
| `internal/config` | Configuration schema (kong tags), XDG config file search paths, validation. |
| `internal/httpserver` | HTTP routing, webhook handlers, alert processing, admin JSON API, the server-rendered admin pages, basic auth and request logging middleware. |
| `internal/httpserver/views` | templ components for the admin UI. The `*_templ.go` files beside them are generated and committed. |
| `internal/routing` | Selects a route for an alert's labels. |
| `internal/templates` | Renders an Adaptive Card from a Go template plus alert data. |
| `internal/graph` | Microsoft Graph client: OAuth2 client credentials, post and update channel messages. `BrokerClient` is the delegated-Teams half (ADR 0037): the same Graph endpoints, called with a per-request Entra bearer token instead of the app-only credential. |
| `internal/bot` | Bot Framework Connector client: a second, separate OAuth2 client credentials flow, send and update activities in a person's chat. See [Bot configuration](#bot-configuration) and [Inbound bot messages](#inbound-bot-messages). |
| `internal/store` | `Store` interface, its SQLite and Postgres backends sharing one adapter; `internal/store/migrations` owns the schema for templates, destinations, routes, recipients, webhook endpoints, active alerts and broker tokens. |
| `internal/models` | Shared data types: `Alert`, `Route`, `Template`, `Destination`, `Recipient`, `ActiveAlert`, `BrokerToken` and the two webhook payload shapes. |
| `internal/httpserver/web` | Embedded static assets: icons, the web manifest and the Tailwind stylesheet built from `views/styles.css`. |
| `internal/cryptutil` | AES-256-GCM sealing for the one thing this service encrypts at rest: the live Keycloak token behind delegated Teams/Channels (ADR 0037). |

Dependencies are injected through constructors — `store.NewSQLiteStore`, `graph.NewClient`,
`routing.New`, `httpserver.NewServer(cfg, store, graphClient, botClient, telemetry)` — so every
component can be exercised with a substitute in tests. There is no package-level mutable state.
`NewServer` takes the unexported `messenger` and `botSender` interfaces rather than `*graph.Client`
and `*bot.Client`, see [ADR 0002](adr/0002-messenger-interface.md).

## Process lifecycle

`main` derives a context from `signal.NotifyContext` for SIGINT and SIGTERM, then calls
`cli.Run`, which parses the arguments and dispatches through `kong.Context.Run`. Commands receive
that context and the parsed configuration.

`ServeCmd` binds its listener before serving, so a bind failure is reported as an error. On
cancellation it calls `http.Server.Shutdown` with `server.shutdown-timeout` (default 15s) to drain
in-flight requests, then closes the store. A second signal kills the process outright. See
[ADR 0003](adr/0003-kong-commands-and-graceful-shutdown.md).

That context reaches the database as well as the handlers: every `store.Store` method takes one, so
a cancelled request stops the query it started and a shutdown does not wait on one. A signal that
arrives while the store is still opening ends the process cleanly rather than reporting a failure
to start. See [ADR 0018](adr/0018-store-takes-a-context.md).

Connections are bounded by `server.read-timeout`, `server.write-timeout` and
`server.idle-timeout`, with the header deadline capped at ten seconds or the read timeout,
whichever is shorter. Without them a slow client holds a connection indefinitely. The write timeout
has to exceed `graph.timeout-sec`, or a handler waiting on Microsoft Graph is cut off before Graph
itself gives up.

The server deliberately sets no `BaseContext`. The only context worth putting there is the one a
signal cancels, and that would cancel every in-flight request the moment SIGTERM arrives — exactly
what the draining shutdown exists to avoid. Request contexts therefore stand alone, and shutdown
waits for the handlers rather than interrupting them.

## Deployment

The container image is built multi-stage: `golang:1.24` compiles a static binary with
`CGO_ENABLED=0`, and the runtime stage is `gcr.io/distroless/static-debian12:nonroot` running as
uid 65532. Configuration is mounted at `/etc/xdg/teamster/config.yaml` — the system-wide XDG path
the binary already searches — and the SQLite file lives in the `/data` volume. See
[ADR 0004](adr/0004-container-image.md).

Images are published to `ghcr.io/<owner>/<repo>` for `linux/amd64` and `linux/arm64`. Every CI
build is tagged with its short commit sha, and branches and pull requests get moving tags
([ADR 0005](adr/0005-image-tagging-and-promotion.md)).

Every image pushed from `main`, and every release image, is scanned with Trivy and the findings
are reported to SecObserve, against the exact digest just built. Pull requests are not scanned,
including same-repository ones that do push an image. A release is reported under its own
SecObserve branch, named after the tag, rather than folded into `main`'s — see
[ADR 0024](adr/0024-trivy-image-scanning.md).

A release rebuilds from the tag after re-running lint and tests, so the version and labels
describe the release; all of its tags share that one build's digest. The image carries SPDX SBOM
and SLSA provenance attestations and is signed with cosign by digest, and the released binaries
ship per-binary SBOMs under a signed `checksums.txt`. `:latest` only ever moves forward, to the
newest stable release. See [ADR 0006](adr/0006-release-rebuild-sbom-signing.md).

### Kubernetes

The Helm chart in [charts/teamster](../charts/teamster) deploys the image, and it is published as
an OCI artifact to `ghcr.io/pflege-de-labs/charts`. The deployment shape is derived from
`database.driver`: a StatefulSet whose `volumeClaimTemplate` carries `/data` for `sqlite`, and a
Deployment for `postgres`, which keeps nothing locally. A sqlite release is one pod, because SQLite
takes a single writer, and rolls with `Recreate`; a postgres release is as many as asked for, rolls
with `maxUnavailable: 0` and a surge pod, and gets a PodDisruptionBudget once there is more than
one. The chart deploys no Postgres of its own — a bundled single-pod database would be less
available than the StatefulSet it replaced — and reads the secret an operator or provider already
made through `database.postgres.passwordFrom`.

Configuration follows the precedence the binary implements. The config file is rendered into a
Secret and mounted at `/etc/xdg/teamster/config.yaml`, while the webhook token, the admin login and
the Graph and OIDC client secrets arrive as `TEAMSTER_*` environment variables from a second
secret — which works only because an environment variable is honoured when no config file sets that
key, so those four stay out of the file. Both secrets are hashed into pod annotations, so a changed
value rolls the pod. The service is published through an Ingress or a Gateway API `HTTPRoute`, and
`extraObjects` carries whatever else a deployment needs. A secret written into `config.settings` is
a render-time error, because a config file value beats the environment variable carrying it. See
[ADR 0023](adr/0023-chart-deploys-either-shape.md).

Gateway API mode renders up to two `HTTPRoute`s along the same trust boundary the server itself
draws: `httpRoute.external` carries the self-authenticating paths (`/webhook/*`, `/teamsv2/*`,
`/bot/messages`), `httpRoute.internal` carries the session- and basic-auth-gated admin UI and API.
`httpRoute.internal` is off by default, and while it is, `httpRoute.external`'s rule set folds in
`httpRoute.internal`'s catch-all, so one route reaches everything — the chart's original
behaviour, kept as the default. Enabling `httpRoute.internal` splits the two onto their own
`parentRefs`/hostnames, which is what lets the admin interface sit behind a private Gateway
listener while the webhooks stay internet-reachable. The fallback runs one way only: enabling
`httpRoute.internal` without `httpRoute.external` does not expose the webhooks. Ingress mode keeps
its single-resource, all-paths shape; the split is Gateway API only. See
[ADR 0033](adr/0033-split-httproute-external-and-internal.md).

## Request flow

```text
POST /webhook/alertmanager        POST /webhook/universal
            │                                │
            └────────► webhookAuth (X-Teamster-Token) ◄───┘
                                 │
                    normalize to models.Alert
                                 │
              fingerprint (payload value, else SHA-256 of
              source + generator + start time + sorted labels)
                                 │
                     routing.Plan(alert.Labels)
                                 │
                 one Delivery per target, kind=channel
                      or kind=recipient (0-2 per route)
                                 │
                    delivery.TemplateID set?
                  yes │                  │ no
         store.GetTemplate      alert.Title/Text/Card
      templates.RenderMessage    (templates.RenderText
                  │               sanitizes Text; Card
                  │               passed through as-is)
                  └────────┬─────────┘
                    title, text, card
                                 │
              ┌──────────────────┴───────────────────┐
         kind=channel                          kind=recipient
    store.GetDestination                    store.GetRecipient
    text as sanitized HTML             text via templates.ToMarkdown
              │                                      │
   ┌──────────┴──────────┐              ┌────────────┴────────────┐
 firing              resolved         firing                 resolved
   │                    │                │                       │
 card known?        card known?      message known?          message known?
 yes → graph.Update yes → graph.Update yes → bot.Update       yes → bot.Send
 no  → graph.Post        + delete row  no  → bot.Send              + delete row
```

A delivery whose route has no `TemplateID` renders from the payload's own `Title`/`Text`/`Card`
instead of a stored template — the fallback `/webhook/universal` gives a sender that already knows
what it wants to say. A route with a template always renders through it; the payload's own fields
are read only when the route has none. See
[ADR 0036](adr/0036-direct-content-when-a-route-has-no-template.md).

A route names up to two targets, so one route produces up to two deliveries and each is claimed,
sent and recorded on its own. A chat resolution **sends** rather than edits, because an edit in
Teams does not re-notify and a silent resolve is the one thing the person on call must not get; a
chat re-fire edits, for the mirror-image reason. See
[ADR 0026](adr/0026-alerts-in-a-persons-chat.md).

The diagram above is `Status` in `{firing, resolved}` — the Alertmanager vocabulary, and the only
values that put a message through the claim protocol at all. Any other `Status`, including none,
takes a third, untracked path instead: render, resolve the destination or recipient, then
`graph.PostMessage` or `bot.SendMessage` once, unconditionally. Nothing is claimed and nothing is
written to `active_alerts` — there is no lifecycle to track, so a repeat post is a second message
rather than an edit of the first. This is `/webhook/universal`'s general case; the firing/resolved
lifecycle is the Alertmanager-shaped specialization of it, and `/webhook/alertmanager` only ever
sends those two values, so its behaviour is unaffected. See
[ADR 0035](adr/0035-a-message-without-a-status-is-delivered-once.md).

Handler errors map to `400` for malformed JSON, `401` for a bad token, and `502` when routing,
rendering, the store or Graph fails.

### Teams V2 webhooks

A second ingest path answers the URL a Microsoft Teams webhook used to have, so a sender pointed at
a Power Automate webhook moves by changing one URL rather than by being rewritten.

```text
POST /teamsv2/{team}/{channel}/{token}
            │
   store.GetWebhookEndpointBySlug  ── not configured ──► 404
            │
   SHA-256 of the token vs the stored digest ── mismatch ──► 401
            │
   teamsv2.Parse: V2 envelope | plain text | legacy MessageCard
            │
   store.GetDestination(endpoint.DestinationID)
            │
   graph.PostMessage ──────────────────────────────────► 200
```

There is no routing and no template: the sender has already decided what the message says and the
URL has already decided where it goes. Nothing is written to `active_alerts` either, because there
is no fingerprint and no status, so there is nothing later to update or resolve.

An endpoint names a `Destination` rather than a Team and a channel of its own, which is what makes
it inherit the Teams picker that filled the destination and the grants that bound who may choose
it. Its token is per endpoint and stored only as a SHA-256 digest; it is shown once, in the answer
to the post that generated it, and rotating it is its own form. See
[ADR 0030](adr/0030-teams-v2-compatible-webhooks.md).

## Authorization

A session carries the roles the claim named, mapped one to one by name and stored on the session
row. Every one of them becomes a parent of the Cedar principal, including roles this build has never
heard of: a deployment that defines its own role and writes a policy for it gets that policy
applied, and a role no policy mentions grants nothing. `admin`, `editor` and `viewer` are the three
the shipped policies define; `auth.default-role` fills in when the claim names none of them, and
without one the user holds no Teamster role and every request answers `403` — as a page that says to
ask an administrator when a browser asked for one.

`internal/authz` holds the rules as Cedar policies embedded in the binary and
evaluates them in-process with `cedar-policy/cedar-go`; roles nest through Cedar entity parents, so
an admin is an editor and an editor is a viewer.

One middleware, `httpserver.authorize`, wraps the admin mux and is the only place a permission is
enforced. It maps the path to a resource type and the method to an action, counting
`POST /api/templates/preview` and `POST /api/routing/match` as reads because they answer a question
and change nothing. `POST /api/recipients/link` is mapped to its own action, `link`, permitted to
`viewer` and up: it binds only the caller's own subject, so a viewer needs it without edit on
anything else — see [Linking a chat](#linking-a-chat). The UI hides what a role may not do, which is
a courtesy rather than a control.

`/api` accepts a session as well as the local credentials; a state-changing call authenticated by a
cookie has to pass the same origin check as a form post, because a cookie travels with a cross-site
request and basic auth credentials do not. See
[ADR 0012](adr/0012-role-based-authorization.md).

### Delivery permissions

A `grants` row scopes a role to a Team or to one channel of it. `authz.ScopeFor` collects the grants
naming any of a session's roles; a session no grant names is **unrestricted**, which is how an
installation behaves before an admin narrows anything. The scope becomes attributes on the Cedar
principal — `unrestricted`, `scopes`, `teams` — and the scoped policies read
`principal.unrestricted || resource in principal.scopes`. A channel entity carries its Team as a
parent, so a grant on a Team reaches the channels in it without the channel list being known.

Three actions are scoped rather than blanket: `deliver`, `viewChannel` and `viewTeam`. Keeping them
separate from `view` and `edit` is what lets a policy narrow the directory without narrowing
everything else a role may do. Admins are covered by the blanket admin policy and are never scoped.

`/admin/permissions` is where an admin sets them: a tree built in the browser from the same pickers
the destination form uses, with `PUT /api/grants/role` replacing everything one role reaches in a
single transaction. The tree is edited as a whole, and sending it as a list of creates and deletes
would leave a half-applied scope behind on any failure.

Enforcement is at the writes — creating or moving a destination, and pointing a route at one — and
the reads that list the directory. `internal/httpserver/grants.go` holds both, and reads the scope
once per response rather than once per entry.

## Routing visualization

`/admin/routing` draws the path an alert takes and answers which routes a set of labels would take.
`GET /api/routing/graph` builds the graph server-side, resolving the identifiers a route stores
into labelled nodes; a route pointing at something deleted becomes a node marked missing rather
than a dropped link, because that broken state is what the view exists to show.

The graph is a directed acyclic graph read left to right: one webhook source node, the root routes,
a column per level of nesting below them, and the destinations last. Every edge means "an alert can
go this way" and says which step it is — `enters`, `refines` or `delivers` — so a dashed
refinement edge can be labelled *as well as* or *instead of* according to the child's greedy flag.
A non-greedy child leaves its parent's delivery edge in place, which is what makes a fan-out
visible as two paths.

A template is **not** a node here: rendering is not a step towards a channel, and drawing it as one
made a single arrow mean two things. A route node carries the template it renders with as a label,
marked inherited when it comes from an ancestor. `GET /api/routing/templates` answers the second,
smaller graph — templates against the routes that use them — which is where a template nothing
references shows up as an orphan.

Each node carries the position it is drawn at, so the layout is decided in Go where the tests can
see it and the browser only draws, zooms and drags. A route node shows the labels it filters for, a
destination node its name together with the Team and channel **names**, resolved through the same
`directoryCache` the pickers use. That lookup is best effort: an unreachable Graph falls back to the
stored ids rather than failing the request.

Matching a label set highlights every path that message takes, not one winning route: the webhook,
every route that matched or delivers, the routes they were reached through, and the channels they
land in are drawn in the match colour while everything else dims — so a fan-out across several
independent routes reads off the same picture as a fan-out through nested children does.

`POST /api/routing/match` returns every route that matched and **why** — `selector`, `default`,
`global-default`, `none` or `no-routes`. That reason comes from `routing.Plan`, which holds the
rule in one place; the delivery path calls the same function. The browser never re-implements
selector matching, so the answer cannot drift from what actually delivers a message.

## Messages

A template renders into a title, formatted text and an Adaptive Card, each optional and at least
one required. `templates.RenderMessage` renders all three; the title is collapsed to one line
because that is what the Teams activity feed previews, and an untitled card falls back to
`templates.DefaultTitle`, the summary line this service sent before templates could name their own.

A route with no template skips rendering entirely: `directMessage` builds the same `title, text,
card` shape straight from `models.Alert.Title`/`Text`/`Card`, the fields a `/webhook/universal`
payload may set directly (`ADR 0036`). It still requires at least one of the three, and still runs
`Text` through the same Markdown sanitizer — the only step it skips is the template lookup.

Rendered text is sanitized in `templates.Sanitize` — parsed with `golang.org/x/net/html` and
written back through an allowlist — before it reaches the Graph client, so every caller gets the
same guarantee. `graph.Message` carries the three parts, and the Graph client assembles the body
with an explicit `<attachment id="1">` where the card goes.

Message text is **authored as Markdown**, rendered to HTML with raw HTML passing through, and
sanitized — which is what keeps templates written before that change working, since `Text` was
documented as HTML. The sanitized HTML is what `graph.Message.Text` carries, exactly as before, and
what `templates.ToMarkdown` emits Bot Framework's Markdown subset from for a chat. One sanitizer,
one trust boundary, two transports. See [ADR 0010](adr/0010-message-shape.md) and its successor
[ADR 0029](adr/0029-templates-are-markdown.md).

## Routing rules

Routes form a tree. `routing.Plan` enters **every root** whose selector matches — every selector key
present with the same value, an empty selector never matching — in descending priority, ties on
name, and walks each matched root's children. Nothing stops two independent roots both naming
`severity=critical`; both fire. The default is a fallback rather than one more candidate: it is
entered only when no non-default root matched at all, never alongside a real match. See
[ADR 0034](adr/0034-fan-out-across-independent-routes.md), which extends
[ADR 0011](adr/0011-nested-routes.md)'s fan-out — there, every matching *child* delivers rather than
one winning; here, every matching *root* does.

The last fallback is the **global default destination**. It is one destination with
`is_default` set, and the first one created gets it. When nothing matched and there is no default
route, which includes the case where no routes exist at all, `Plan` returns a single channel
delivery to that destination. The reason is `global-default`, the route is the synthetic
`routing.GlobalDefaultRouteID`, and there is no template. `none` and `no-routes` are now returned
only when no destination exists. The admin route list and the routing picture draw the synthetic
route last and offer no way to edit it. Only the admin role may choose a different default, and
the current default cannot be deleted while other destinations remain. See
[ADR 0038](adr/0038-global-default-destination.md).

A child is evaluated only once its parent matched, and applies when its own selector matches. Every
matching child delivers; a greedy one delivers *instead of* its parent, a non-greedy one *as well
as* it. An unset destination, recipient or template is inherited from the nearest ancestor that sets
one, so "the same card, one more channel" is a route with a single field. The walk stops at
`routing.MaxDepth`.

A route has **two independent targets**, not a choice between them: a `destination_id` for a Team
channel and a `recipient_id` for a person's chat. A route naming both fans out to two deliveries —
channel first, then recipient, an order callers index into — and one naming neither, inherited or
its own, delivers nothing at all. Greedy remains one flag per parent: a greedy child suppresses
**both** of its parent's deliveries, never one of them.

`Plan` returns a `Result`: the reason, every root that matched, and one `Delivery` per message with
its `Kind`, its target and its template resolved. Each `Delivery` already names its own route, root
or child, so nothing downstream of `Plan` needs to know or care how many roots contributed to the
list. `routing.ValidateRoute` and `ValidateDelete` are called from both write paths and keep the
tree acyclic, bounded and free of children that could never fire.

## Configuration transfer

`internal/transfer` reads the configuration into a versioned bundle and writes one back. It carries
templates, destinations, routes and grants — no credentials, no sessions, no alert state — and
preserves ids, so a bundle re-imported where it came from changes nothing.

Recipients are excluded on purpose: a link binds one person to one conversation in one tenant, so it
means nothing in the installation a bundle is carried to. A bundle whose route names a recipient is
rejected on import rather than imported inert.

An import validates the whole bundle before writing anything: the version, duplicate ids, references
that point outside the bundle, and cycles in the route tree, the last through
`routing.ValidateRoute` so the rules live in one place. It then applies it inside `store.WithTx`,
writing templates and destinations before the routes that point at them and routes parents first. A
dry run runs that same import and rolls it back, rather than computing the diff a second way.

`store.Store` grows `WithTx(ctx, func(ctx, Store) error) error` for it. The store and the
transaction-bound view of it embed the same generated queries, so the data methods are written
once; `Close`, `Ping` and a nested `WithTx` are refused inside a transaction, by a type that has no
database to perform them with rather than by a nil check. `WithTx` passes
the context to the function rather than letting it close over the caller's, so a transaction can be
given a deadline of its own without changing any caller.

`GET /api/config/export`, `POST /api/config/import` and the `teamster export` / `teamster import`
commands are three doors onto the same code. See
[ADR 0013](adr/0013-configuration-transfer.md).

## Metrics

`internal/metrics` owns one OpenTelemetry pipeline and as many readers as the configuration asks for:
the Prometheus exporter, an OTLP exporter, both, or neither — neither being the default. A disabled
pipeline is a usable value backed by the no-op meter provider, so every call site records
unconditionally; only the HTTP wrappers branch, because instrumentation does per-request work before
it discovers the instrument is a no-op.

One view, matched on instrument kind, aggregates every histogram as base-2 exponential. That is what
the Prometheus exporter renders as a native histogram — it maps nothing else — and it means a
histogram added later is native without anyone revisiting the decision. See
[ADR 0017](adr/0017-metrics-through-opentelemetry.md).

`otelhttp` records the two semantic-convention histograms: the server one wraps the whole handler
chain, the client one sits **below** the Graph client's oauth2 transport so a token refresh is not
billed to Graph. The route attribute needs `metrics.RouteTag` beside the mux: `otelhttp` reads
`http.Request.Pattern`, `ServeMux` sets it in place, and the middleware in between hands the mux a
copy — so the wrapper on the outside would see an empty pattern and record a metric with no route and
no test failing. `RouteTag` writes into the labeler, which lives in the context and survives being
copied.

`/metrics` has a listener of its own and must keep it: the main mux's catch-all is behind
`requireSession(authorize(…))`, so mounting it there would hide it from every scraper. The listener
binds in `Start` rather than in a goroutine, so a port already in use is an error `serve` returns.

Shutdown order is the reason the defers in `internal/cli/serve.go` are registered store → metrics →
listener: LIFO runs them in reverse, so the server drains, the scrape endpoint closes, the final OTLP
export goes out, and only then does the database close — which the active-alerts gauge reads on every
collection.

## Probes

`GET /healthz` and `GET /readyz` answer before any authentication, because a kubelet has no
credentials. They are separate questions: liveness is whether this process should be killed, and it
therefore checks nothing outside the process; readiness is whether it can serve, and it checks the
store with `store.Ping` and reports "shutting down" once `http.Server.RegisterOnShutdown` has fired.
Graph is not checked — an instance whose Graph calls fail is exactly the instance an operator wants
to reach.

The draining flag is set from a shutdown hook, which net/http runs in its own goroutine; readiness
therefore flips a moment after `Shutdown` is called rather than during it, which is immaterial
against a probe interval and is why the test for it waits rather than assuming an order.

## Invariants the database holds

Three rules are enforced by the store rather than by the code above it, because
a check and the write it guards are otherwise two steps that another writer can
get between.

A role granted the same scope twice is a duplicate, not a second permission, so
`(role, team_id, channel_id)` is unique and replacing a role's scope is a set
delete rather than a list and a loop. `recipients.subject` is unique for the
same kind of reason: a person with two bindings would be sent every alert twice,
so re-linking updates the row that exists. Route writes and deletes run their cycle
and orphan checks inside `WithSerializableTx`, which is what stops two admins
from each validating against a tree the other is about to change -- an orphaned
child becomes a root, and a root matches the alerts its parent used to filter
out. No constraint can express "this tree has no cycle", which is why that one
is a transaction rather than an index.

## Two backends, one adapter

`store.Open` picks the backend from `database.driver`, by name and never by inference: the
container image sets `TEAMSTER_DATABASE_PATH` whatever the driver is, so guessing would let an
operator who configured only Postgres run happily against a file that dies with the container. The
startup log says which database was opened.

sqlc emits a package per dialect, and Go has no structural satisfaction across packages, so the two
generated `Querier` interfaces can never be one type. Their parameter and row structs are generated
from the same queries and are field for field identical, though, which makes them convertible — so
the mapping between rows and `internal/models` lives once in `queryAdapter`, and `pgqueries.go` is
one conversion per statement. The Postgres query files are themselves generated from the SQLite
ones, which differ only in how a parameter is spelled.

What Postgres adds is what a networked, genuinely concurrent database needs and a file does not: a
bounded pool with a connection lifetime, session timeouts for statements, locks and idle
transactions, isolation asked for by name, a bounded retry for the conflicts serializable isolation
reports rather than prevents, and an advisory lock so two instances starting together cannot both
migrate. See [ADR 0022](adr/0022-postgres-second-backend.md).

## Queries

The statements live in `internal/store/queries/<dialect>/` and the Go that runs them is generated
from them by sqlc, type-checked against the migrations, and committed — so a build needs no
generator, the same bargain the admin UI makes ([ADR 0008](adr/0008-templ-tailwind-admin-ui.md)).
`internal/store/sqlite.go` is the adapter between the generated rows and `internal/models`, and
`internal/store/policy.go` holds the handful of rules that are decisions rather than SQL: what an
empty id means, which clock stamps a row, when a row that exists still reads as missing.

Editing a `.sql` file means running `make generate`. A stale regeneration compiles and runs the old
statement, which is why CI regenerates and diffs. See [ADR 0020](adr/0020-sqlc-generated-queries.md).

## The schema

`internal/store/migrations` owns the schema: numbered files per dialect, embedded in the binary and
applied by goose. Two exist so far. `0001` is the baseline — the schema as it stood when the ledger
was introduced, written with `IF NOT EXISTS` so that a database an earlier build already converged
adopts it rather than failing. `0002` is a Go migration holding the convergence those earlier
builds performed on every open: add the columns a later release introduced, rebuild `active_alerts`
if it still carries the old single-column key, and refuse a database whose timestamps are declared
the wrong type. It runs once and is recorded, which is what retires the `PRAGMA` inspection that
used to run on every start.

`recipients` and `link_flows` were added later, by `0005` in SQLite and `0002` in Postgres. A
recipient is a person who asked for their alerts as a chat message rather than only in a channel,
and the row holds the Bot Framework conversation reference needed to send one unprompted. Its
`bot_channel_id` is a Bot Framework channel — `msteams` — and not a Teams channel, which is what
`channel_id` means in every other table. A link flow is the one-time code that binds a conversation
to a person, redeemed by deleting the row exactly as `login_flows` is. Nothing delivers to a
recipient is a person who asked for their alerts as a chat message rather than only in a channel.

`0006` in SQLite and `0003` in Postgres then join the two: `routes` gains a nullable-by-default
`recipient_id`, and `active_alert_recipients` carries the claim protocol for chat delivery. It is a
sibling of `active_alerts` rather than a wider key on it because a person has neither a `team_id`
nor a `channel_id` for that table's CHECK to hold. Both changes are additive, so the previous
release runs against the new schema unchanged. The decision behind all of it is the ADR on alerts in
a person's chat, in the [ADR index](adr/README.md).

`0007` in SQLite and `0004` in Postgres add `recipients.blocked_at` (nullable) and `.blocked_reason`
(`NOT NULL DEFAULT ''`), both additive. See [Alert lifecycle](#alert-lifecycle) for what the flag
means and [Managing recipients](#managing-recipients) for where it is read.

`webhook_endpoints` followed, by `0008` in SQLite and `0005` in Postgres. A row is one Teams V2
webhook URL: the two slugs that name it, the destination it posts into, and a SHA-256 digest of the
token the sender puts in the path. The pair of slugs is unique, because the pair is the URL. The
token itself is nowhere in the database, so rotating is the only way to recover from a URL that
leaked.

`0011` in SQLite and `0008` in Postgres add `destinations.is_default`
(`NOT NULL DEFAULT false`), a partial unique index `WHERE is_default` that allows at most one
default, and a backfill that marks the oldest destination. `CreateDestination` sets the flag in the
insert itself whenever no default exists, so the previous release can run against this schema and
even delete the default. The next destination created then takes over.

`database.migrate` decides what opening the store does about a schema that is behind: `auto`
applies what is missing, `verify` refuses and names `teamster migrate up`, `off` asks nothing.
`teamster export` always verifies — reading a database must not migrate it. A migration must leave
the previous release able to run against the new schema, because migrations run before the pods
that need them. See [ADR 0019](adr/0019-goose-migrations.md).

## Alert lifecycle

Timestamp columns are declared `DATETIME`; the SQLite driver only converts them back to
`time.Time` for that declared type. A migration refuses a database whose timestamp columns are
declared otherwise, since every read from it would fail.

`active_alerts` is keyed by `(fingerprint, team_id, channel_id)` and stores the Graph message ID, so
an alert that fans out has one row per channel. A repeated `firing` alert edits the existing card in
each channel instead of posting a new one, and a `resolved` alert edits each card one last time
before its row is deleted.

A row exists from the moment delivery claims the right to post, which is before the card does:
`posted_at` is non-NULL exactly when a card exists, and a CHECK ties it to the message id so the
half state cannot be written. Delivery claims the row, calls Graph outside any transaction, then
completes the claim — a transaction spanning a network call would pin a connection for as long as
Microsoft takes and would tell nobody anything if the process died holding it, whereas the claim
row is the record that a post was in flight. A claim whose owner never completed it is taken over
by the next attempt once `max(30s, 3 x graph.timeout-sec)` has passed, so recovery is lazy and
there is no reaper. A caller that finds a claim in flight is refused with a 502, and the sender's
retry lands on the card the winner created. See
[ADR 0021](adr/0021-claim-a-card-before-posting.md).

Delivery is best effort per target: one channel or one person failing does not cost the others their
message, and the failures are reported together as a `502`. That is safe because each delivery
records its own message id, so a retry updates the messages that made it rather than duplicating
them. Resolution walks the stored rows rather than the plan — **both tables** — so a message the
routes no longer name still stops claiming the alert is firing.

`active_alert_recipients` keys `(fingerprint, recipient_id)` and mirrors that protocol statement for
statement, with one deliberate difference. `bot.SendMessage` returning `("", nil)` is a documented
success — delivered, but with nothing to name it by for a later edit — so this table's CHECK admits
a posted row with an empty activity id, where `active_alerts` would read it as never posted and send
a duplicate on the next firing. The update path skips the edit when there is no id rather than
failing.

A chat failure is sorted into permanent and transient. `MessageWritesBlocked`, or
`ConversationBlockedByUser` one level in, means the person uninstalled or blocked the bot: the row
is dropped and the delivery counted under its own `blocked` outcome, so it stops re-attempting
within that alert and is visible as more than noise. Everything else — 429, any 5xx, a transport
error — fails that delivery as a channel failure does and leaves the row for the sender's retry.

A permanent failure also stamps `recipients.blocked_at` and `.blocked_reason` (the API error's own
code, e.g. `MessageWritesBlocked`), through the narrow `MarkRecipientBlocked` statement rather than
the general-purpose `UpdateRecipient` — delivery only ever read the conversation reference, and a
narrow statement is what stops a delivery failure from clobbering a field it never loaded. The flag
is informational and self-healing, never a gate: `ClearRecipientBlocked` runs after every
successful send or update, and a blocked recipient is attempted again on the next alert exactly like
one that never was. Gating delivery on it would trade a visible problem for an invisible one — a
person who reinstalled the bot would otherwise receive nothing again until an admin happened to
notice the flag and clear it by hand. `/admin/recipients` is where an admin reads it; see
[Managing recipients](#managing-recipients).

A database whose `active_alerts` is keyed by fingerprint alone is rebuilt on the next start; SQLite
cannot change a primary key in place. The rows survive.

## Signing in

`/admin` requires a session. Two ways to get one, both ending in a row in the `sessions` table and
an opaque id in an `HttpOnly`, `SameSite=Lax` cookie, marked `Secure` when the request arrived over
TLS or carried `X-Forwarded-Proto: https`:

* An OIDC authorization code flow with PKCE. The provider is configured by the URL of its
  `/.well-known/openid-configuration`, and the issuer that ID tokens are verified against is read
  from that document rather than configured separately. `state`, `nonce` and the
  PKCE verifier live in `login_flows` rather than a cookie, and taking a flow deletes it, so a
  replayed or forged callback finds nothing. What a user may do comes from a claim: `auth-claim`
  is a dotted path, because Keycloak nests roles under `realm_access.roles`, and its values are
  matched against the role names. The claim is looked for in the ID token, then at the userinfo endpoint,
  then in the access token, first hit winning, because Keycloak's built-in role mappers populate
  the access token and leave the ID token without roles. See
  [Configuring Keycloak](keycloak.md).
* The local username and password, entered at `/admin/login`. This is the bootstrap path and the
  way back in when the provider is unreachable or the claim is misconfigured.

`/api` keeps HTTP basic auth: automation cannot complete an authorization code flow. Expired
sessions and abandoned flows are swept hourly, and neither is honoured once expired regardless.

None of this touches `internal/graph`, whose Entra credentials are a machine credential for posting
cards. Alert delivery does not depend on anyone being signed in. See
[ADR 0009](adr/0009-admin-authentication.md).

## Admin UI

`/admin` is rendered on the server by `internal/httpserver/views`, compiled from templ sources and
styled with Tailwind. Forms post to `/admin/{templates,destinations,routes}` and their `/delete`
variants and answer `303 See Other`, so the result is a normal page load. `/admin` also takes
`edit` and `id` query parameters, which load one record into its form so the same endpoint
updates instead of creating. Those endpoints require
the request to prove its origin, because basic auth credentials travel with a cross-site post and
there is no session to hold a CSRF token. See
[ADR 0008](adr/0008-templ-tailwind-admin-ui.md).

`formPost` takes the redirect target as a parameter rather than always answering `/admin`: a page of
its own — `/admin/recipients`, like `/admin/permissions` — posts to its own `/delete` endpoint and
lands back on itself with the notice, not on the unrelated configuration page.

### Managing recipients

`/admin/recipients` lists every recipient, the routes that target them by name, and the blocked
state described in [Alert lifecycle](#alert-lifecycle). Viewing needs `authz.ActionView` and
unlinking `authz.ActionEdit`, both on `authz.Resource{Type: "Recipient"}` — no new Cedar action, the
existing admin/editor/viewer policies already cover both on any resource type. `POST
/admin/recipients/delete` unlinks through `formPost`, and is permitted even when a route still names
the recipient: the page having just shown which routes do is what makes that an informed choice,
matching how deleting a destination already works. `GET /api/recipients` and `DELETE
/api/recipients/{id}` are the same two operations over the API, behind the same authorization; the
response type omits `ConversationID`, `ServiceURL`, `AADObjectID` and `TenantID` — the Bot Framework
conversation reference is operational plumbing an admin unlinking someone has no use for.

`POST /api/templates/preview` renders a template body against a built-in sample alert through the
same `templates.Render` the delivery path uses, and returns the Adaptive Card JSON. The browser
draws it with the vendored renderer in `web/vendor`. A template that fails to render comes back as
an error field with status 200, because a broken template is the answer the operator asked for.
That directory's contents are pinned by package and version in `manifest.json`, and
`internal/httpserver/vendor_test.go` verifies each file's SHA-256 against it and that the directory
and the manifest list the same files. `make vendor` re-downloads at the pinned versions from the
npm registry, checking each tarball against the `dist.integrity` the registry publishes for that
version before extracting anything from it. CI runs it on every push because a version bump alone
leaves the stale file and its checksum agreeing with each other; see
[ADR 0031](adr/0031-vendored-browser-libraries-pinned-and-verified.md).

`GET /api/graph/teams` and `GET /api/graph/teams/{id}/channels` read the tenant's teams and
channels through the Graph client, behind a five minute in-memory cache because the Graph throttles
and those lists barely change. Failures are not cached. The destination form ships its Team and
Channel fields as ordinary text inputs and a script upgrades them to name-based selects once those
endpoints answer, so a missing permission, a throttle or an outage costs the convenience rather
than the ability to configure a destination.

### Delegated Teams and channels

`GET /api/graph/my-teams` and `GET /api/graph/my-teams/{id}/channels` are the tenant-wide pair
above, mirrored for the signed-in admin's own Teams and channels rather than everything the
app-only credential can see. Neither goes through `directoryCache`: the result is per admin, not
tenant-wide, so caching it the same way would leak one admin's Teams into another's picker. Both
require `auth-broker-enabled` and a session whose `Source` is `oidc` — a local login was never
federated through Keycloak and has nothing to ask for — and answer `409` otherwise, which
`pickers.js` treats as "offer only the tenant-wide list."

The Entra token behind these calls is never obtained from Entra directly. Keycloak's Entra identity
provider link, with **Store Tokens** enabled, keeps the upstream Entra token from the login that
created each session; `GET {issuer}/broker/{alias}/token`, bearing that session's own Keycloak
access token, returns it — refreshing it upstream itself if needed. `internal/httpserver/broker.go`
keeps only a live Keycloak access and refresh token per session, in a new `broker_tokens` table
keyed by session id, sealed with AES-256-GCM (`internal/cryptutil`) under a key from
`auth-broker-token-encryption-key`, the session id as additional authenticated data so a row cannot
be decrypted as if it belonged to a different session. Refreshing it is ordinary OAuth2 against
Keycloak's own token endpoint (the same one `oidc.go` already discovered for login), and a
refreshed token is written back re-encrypted so the next request does not refresh again for
nothing. Keycloak firmly rejecting the refresh token — expired or revoked — deletes the row, so the
next attempt fails fast rather than retrying a dead credential forever; a transient failure to
reach Keycloak leaves it in place. `internal/graph.BrokerClient` is the Graph half: a plain
`*http.Client` with a bearer header set per call from whatever Entra token was just obtained, kept
separate from `Client`'s own `clientcredentials`-wrapped one because the two credential shapes do
not mix. See [ADR 0037](adr/0037-delegated-teams-via-keycloak-broker-token.md) and
[Configuring Keycloak](keycloak.md).

Regenerating the UI needs `make generate`, which runs templ and Tailwind. Their output is
committed, so building the service does not.

## Admin API

Everything outside `/webhook/*`, `/teamsv2/*` and `/bot/messages` sits behind HTTP basic auth using
`admin-username` and `admin-password`, or a session. `/admin` serves the UI; `/api/templates`,
`/api/destinations`, `/api/routes` and `/api/webhooks` (plus their `/{id}` variants) provide CRUD
over the stored configuration. `POST /api/webhooks/{id}/rotate` mints a new token, and is the only
other place one is ever readable: creating and rotating answer with the token and the full URL,
every later read leaves both empty. `GET /api/recipients` and `DELETE /api/recipients/{id}` list and
unlink recipients — see [Managing recipients](#managing-recipients) — and
`POST /api/recipients/link` is the exception that requires a session specifically — see
[Linking a chat](#linking-a-chat).

## Configuration

Layered, lowest precedence first: kong tag defaults → `TEAMSTER_*` environment variables →
`$XDG_CONFIG_DIRS` files → `$XDG_CONFIG_HOME` file → `./config.yaml` → `--config FILE` →
command line flags. Kong applies resolvers before env-backed defaults, so a config file value
beats the matching environment variable.
See [README](../README.md#configuration) for the concrete paths and
[ADR 0001](adr/0001-kong-xdg-configuration.md) for the reasoning.

## Bot configuration

`config.BotConfig` (`bot.*` / `TEAMSTER_BOT_*`) holds a second Entra registration, separate from
`GraphConfig`, for the Bot Framework identity that will send alerts to a person's chat
([ADR 0026](adr/0026-alerts-in-a-persons-chat.md)). `config.Validate` gates the whole feature on
`bot-tenant-id`, `bot-client-id` and `bot-client-secret` being set together — all three empty
turns the feature off, and setting only one is a startup error — mirroring how metrics are gated
on `metrics.enabled` today. Once the feature is on, `bot-metadata-url` is required and must be
`https`: it is the trust anchor every inbound activity is checked against, so an empty or
non-`https` value is refused at startup rather than registering a route that would 502 forever.

`internal/bot.NewClient` mirrors `graph.NewClient`: an `oauth2/clientcredentials` flow with the
instrumented transport injected below oauth2's, so a token refresh is measured against the token
endpoint rather than billed to the Bot Connector. `bot.TokenURL` derives
`https://login.microsoftonline.com/<bot-tenant-id>/oauth2/v2.0/token` for `bot-tenant-type: single`,
or the shared `https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token` for `multi`,
unless `bot-token-url` overrides it — a multi-tenant bot registration authenticates through that
shared tenant rather than its own, and getting this wrong 401s every send.

`config.BotConfig.Configured()` is the single place that decides whether the feature is on: the same
three-field check `botConfigured` (`internal/httpserver/bot_auth.go`) used to run inline now lives on
the config type so both the HTTP layer and `ServeCmd` share it rather than risking two gates drifting
apart. `ServeCmd` constructs a `bot.Client` only when it reports true — unlike an earlier version of
this code, which built one unconditionally on the reasoning that `NewClient` never fails on an empty
configuration. Building it regardless left `httpserver`'s `if s.bot == nil` guards
(`bot_messages.go`'s `replyText`/`notifyConversationDisplaced`, `notifications_page.go`'s
`notifyUnlinked`) dead outside a test that deliberately passes `nil`, and meant an unconfigured
deployment's unlink button issued a real `clientcredentials` token request against three empty
strings, and logged a failure, on every use. `ServeCmd` holds the client in a locally declared
interface variable with `botSender`'s exact method set rather than a `*bot.Client`, left as a true nil
interface when the feature is off: assigning a nil `*bot.Client` to `httpserver.NewServer`'s
`botSender` parameter directly would instead produce a non-nil interface holding a nil pointer, which
compares unequal to `nil` on the other side and would defeat every one of those guards the same way.

What is conditional on the same switch is every route this feature adds: `POST /bot/messages`, and
`/admin/notifications` with its actions and nav link (see
[Self-service: /admin/notifications](#self-service-adminnotifications)) — a deployment that never
turns the feature on exposes no unauthenticated path and offers nobody a page instructing them to
talk to a bot that isn't there. See [Inbound bot messages](#inbound-bot-messages) for what validates
a request once the route exists, and [Linking a chat](#linking-a-chat) for how a person ends up
receiving anything through it.

## Inbound bot messages

`POST /bot/messages` is authenticated by **Microsoft's** signature, not by any of teamster's own
credentials — the webhook token and the admin session/basic-auth both mean nothing here. It sits on
the plain mux beside `/webhook/*`, registered without a method in its pattern so its own internal
check produces a `405` for a `GET`: the mux's catch-all `/` route (`requireSession`) would otherwise
answer a non-`POST` with a redirect to the login page rather than a `405`.

`internal/httpserver/bot_auth.go` implements the [Bot Framework authentication
spec](https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-authentication)
end to end. Order matters, and the reasons are in the code:

```text
POST /bot/messages
        │
http.MaxBytesReader (256 KiB)
        │
decode JSON, then the cheap gates (no bearer token touched yet):
  channelId == "msteams", type in {message, conversationUpdate},
  conversation.conversationType == "personal",
  channelData.tenant.id (if present) matches bot-tenant-id (single-tenant only)
        │
parse "Authorization: Bearer <token>"
        │
fetch bot-metadata-url once, cache issuer + jwks_uri (like oidc.go's discovery)
        │
oidc.NewVerifier(issuer, rateLimitedKeySet(RemoteKeySet), ClientID, RS256, Now = time.Now()-5m)
        │
claims.serviceurl: reject if empty, else trim one trailing slash and compare == activity.ServiceURL (same trim)
        │
fetch the raw JWKS once, cache kid → endorsements; refetch once on an unknown kid
        │
activity.ChannelID must appear in the signing key's endorsements (absent member = none)
        │
        ▼
dispatchBotActivity (conversationUpdate / message)
```

The gates run before the bearer token because a JWT's `kid` header is attacker-controlled and read
before any signature check; an unknown `kid` makes go-oidc's `RemoteKeySet` fetch this process's own
JWKS cache from the network, so a request that never had a chance of being genuine (wrong channel,
wrong type, wrong conversation type, wrong tenant) is refused before paying for that. A shape
refusal answers `200` and drops the activity rather than `401`: Microsoft delivered and signed it
correctly, and any status but 2xx reads to its tooling as "this bot's auth is broken" rather than
"this bot ignores this activity" — each refusal reason is still counted separately under
`metrics.WebhookReceived`. Backdating the verifier's clock five minutes covers Microsoft's stated
clock-skew allowance for `exp`, which go-oidc otherwise gives zero leeway; the accepted side effect
is that `nbf` — which go-oidc already gives five minutes — tolerates ten. The service URL comparison
is `==`, never `HasPrefix` or `Contains`, and an empty claim is rejected before the comparison so it
cannot match an equally empty body field: `ServiceURL` is concatenated into the outbound URL that
carries this service's own Bot Connector bearer token, so a prefix match — or a no-op match on two
absent values — would send that token to an attacker-chosen host.

`rateLimitedKeySet` (`bot_auth.go`) wraps the `RemoteKeySet` go-oidc builds: go-oidc singleflights
*concurrent* callers for the same kid but puts no floor between *sequential* ones, so a `kid` an
attacker increments on every request would otherwise force one outbound JWKS fetch per request —
enough to get this process throttled by Microsoft, taking genuine traffic down with it (a
self-inflicted denial of service, unauthenticated). A kid that verifies is remembered so real
traffic never pays again; an unrecognised kid is allowed through only `keyFetchBurst` times per
`keyFetchFloor` window — a burst rather than a single one-ever attempt, because Microsoft keeps more
than one signing key valid at once during a rotation, and the second, equally legitimate key of an
ordinary overlap must not be treated as the flood this defends against. A kid denied by the floor
fails closed. Similarly, `verifierFor` and `endorses` never hold their mutex across the network
fetch that populates them (a mutex ignores context cancellation, so holding one across a 15s call
would let disconnected clients' goroutines pile up without bound); concurrent callers on a cold
cache wait on a channel instead, and a failed fetch is cached for `negativeCacheTTL` so a blackholed
IdP costs one timeout every 30s rather than one per request.

Every refusal is counted through `metrics.WebhookReceived(ctx, "bot", status)` with a distinct
status string — a missing bearer, a failed signature/issuer/audience/expiry check, and an
unendorsed channel are separately countable, the last one answering `403` rather than `401` because
the signature is valid and the token simply does not speak for this channel. A metadata or JWKS
fetch failure answers `502` rather than `401`, because it is this service's own egress that failed,
not evidence of a forged request, and it is worth Bot Connector retrying. A JWKS key carrying no
`endorsements` member speaks for no channel — the fail-closed reading. The live document was
checked rather than reasoned about: all 220 keys at `login.botframework.com/v1/.well-known/keys`
carry the member, none of them empty, and 74 endorse `msteams`. An absent member is therefore not
something Microsoft serves, and reading it as "valid for every channel" could only ever weaken the
one check the spec calls mandatory.

Once every check passes, `dispatchBotActivity` always answers `200` — the Bot Connector retries
anything else, and no failure past this point is the kind a retry fixes:

* `conversationUpdate` with the bot's own id (its Teams id in this conversation, `28:<app-id>`, read
  from `recipient.id` rather than the configured App ID) among `membersAdded` replies with
  install instructions and writes nothing: an install is not consent.
* `conversationUpdate` naming the bot itself among `membersRemoved` retires the link for that
  conversation and replies with nothing — the bot has just been removed, so there is nowhere to
  reply to. Any other member leaving is ignored. See
  [ADR 0032](adr/0032-retiring-a-link-from-the-chat.md).
* `message` that *is* `unlink`, `stop` or `unsubscribe`, once the bot's mention is stripped,
  retires the link for that conversation and confirms in the chat. Whole-message, not a search:
  `unlink` is an ordinary word where a link code is not, so "how do I unlink this chat?" does not.
* `message` normalizes the text — strips `<at>...</at>` mention markup, using `entities[]` where
  present; unescapes HTML entities; upper-cases; then finds the first run shaped like a link code —
  and tries to redeem it. See [Linking a chat](#linking-a-chat).
* Every activity that reaches this point (a `message`, and a `conversationUpdate` that is not the
  bot's own install) refreshes a known recipient's `ServiceURL` from the activity, if the
  conversation already belongs to one. This is the only signal Teams gives that a conversation moved
  to a different regional endpoint, and `message` is what actually arrives for a personal-scope bot
  — a `conversationUpdate` beyond the initial install is close to never.

Both retirement paths resolve the conversation to a recipient with
`GetRecipientByConversation` and delete through `store.DeleteRecipient`, which cascades the
active-alert rows in its own transaction. Controlling the chat is the whole authorization: linking
grants and therefore needs a code minted by an admin-UI session, while unlinking only ever revokes,
and only for the conversation the activity arrived on.

## Linking a chat

`POST /api/recipients/link` mints a one-time code bound to the caller's own subject, for that
person to type or paste to the bot in their 1:1 Teams chat. It requires a real session —
`currentSession`, not the principal a middleware already attached to the request — because
`basicAuth` authenticates every script as the *configured admin username*, and minting a code under
that subject would bind a chat to "the admin account" rather than to whoever holds the shared
password. A basic-auth caller gets a `403` naming the reason.

Authorization maps the path to `authz.ActionLink` on a `Recipient` resource, permitted to
`Role::"viewer"` and up — the on-call person this feature is for is typically not an editor.
`requestAuthorization` special-cases the path; without that it would fall to the generic
`Page`/`edit` mapping, which a viewer is refused.

The code is twelve symbols from a 31-symbol alphabet excluding `0/O` and `1/I/L` (~59.4 bits), drawn
with `crypto/rand` via `math/big.Int`'s rejection sampling — needed because 31 does not divide
evenly into 256, and rejection sampling keeps the draw unbiased regardless — and displayed grouped
as `XXXX-XXXX-XXXX`; stored and matched without the dashes. Minting one first deletes every code the
subject already had outstanding — otherwise old ones pile up until the hourly sweep and each live
one widens what a guess has to beat — and retries on a primary-key collision
(`store.ErrConflict`) rather than surfacing it as a `500`. It expires after ten minutes, the
transcript it sits in being reason enough to keep it short.

Redemption spends the code first: `TakeLinkFlow` runs on the plain store, not inside a transaction,
so the row is deleted whether or not anything after it succeeds — nesting it inside a transaction
that could later roll back (an expired code, or a failure in the create/update that follows) would
silently un-spend it, letting the same code be retried. `GetRecipientBySubject`, then an update or a
create, then runs inside one `store.WithSerializableTx`: two live codes for the same subject
redeemed concurrently would both miss the lookup and both try to insert, which only the unique index
on `subject` catches, after the fact. `Subject` on the resulting `Recipient` comes from the redeemed
`LinkFlow` alone, never from the activity, which is only ever the phone somebody happened to be
holding. An unrecognised or expired code gets the same neutral reply either way, so a guess learns
nothing about how close it was.

Redemption also carries two updates for whoever already held the subject's link: `channelData`'s
absent tenant (Teams omits it for some activity shapes) never overwrites a previously-known-good
one, and a redemption that moves the conversation — the shape a leaked code takes when it hijacks
someone else's alert stream — sends the *previous* conversation a notice that it was displaced, via
the bot client, once the new binding has committed. See [ADR 0026](adr/0026-alerts-in-a-persons-chat.md)
for why this feature exists at all; unlinking, below, is what an adversarial review of the previous
PR found missing from it. A person can unlink themselves there; an admin can unlink anyone from
[Managing recipients](#managing-recipients).

### Self-service: `/admin/notifications`

`GET /admin/notifications` is a page like any other under `requireSession`, offered to `viewer` and
up: linking or unlinking a chat is a person managing their own membership, not administering
anyone else's, which is why it is not folded into `/admin` or gated behind `CanManage` the way the
configuration and permissions panels are. It, its two POST actions and the nav link to it
(`layout.templ`, gated on `Viewer.NotificationsEnabled`) are registered/rendered only when
`config.BotConfig.Configured()` reports the bot is on — see the bot configuration section above for
why: a code minted here can never be redeemed on a deployment that never registered the endpoint it
would be typed into.

`handleNotificationsPage`, `handleMintLink`, `cancelLink` and `unlinkNotifications`
(`internal/httpserver/notifications_page.go`) resolve *who* is asking from `currentSession(r).Subject`
— never from the principal a middleware already attached, and never from an id a form carries — for
the same reason `handleLinkRecipient` does: `basicAuth` authenticates every script as the configured
admin username, and nothing under `/admin/` treats it as a session in the first place, so a
basic-auth-only request is redirected to sign in by `requireSession` before it ever reaches these
handlers. `TestNotificationsHandlersResolveIdentityFromTheSessionNotThePrincipal` pins this by calling
the handlers directly with a principal manufactured to disagree with the session — through the real
mux the two are always equal, since `requireSession` is the only thing that ever sets the principal
and always sets it from that same session, so no request built through the mux can tell a
`currentSession(r)` implementation apart from a `principalSubject(r)` one.

`requestAuthorization` maps every path that is `/admin/notifications` or starts with
`/admin/notifications/` — anchored on the trailing slash, not a bare prefix, so an unrelated future
path merely starting with the same characters (`/admin/notification-rules`, say) does not inherit
this mapping by accident — to `Recipient`. A `GET` falls through to the generic method-based mapping
below it and gets `authz.ActionView`, the same as any other page a viewer may read; only the two POSTs
(mint, and unlink) plus the cancel action get `authz.ActionLink`, the mapping `/api/recipients/link`
already used. Mapping the read to `view` rather than `link` matters for a deployment that defines
its own role and permits `view` without `link`: that role sees the nav link (unguarded, since it is
offered to every signed-in person) and must not then get a bare `403` reading it.

The page never renders `Recipient.ConversationID` or `.AADObjectID`: both are opaque Bot Framework
and Azure AD identifiers with no reason to be in a screenshot or a support ticket. `Recipient.Name`
— Teams' display name for whoever last redeemed a code, attacker-controlled and unbounded — is
truncated to `maxDisplayNameLength` (80 runes) before it reaches the page. What it shows is the
truncated name, `CreatedAt` ("linked since") and, only when it differs from `CreatedAt`, `UpdatedAt`
("last changed") — worded to say plainly that a service-url refresh (`refreshRecipientServiceURL`)
updates the same field a new redemption does, so a change there alone is not proof of a takeover,
only something worth noticing. What it offers is a button that calls `handleMintLink`, one that
posts to `cancelLink`, and — only once linked — one that posts to `unlinkNotifications`.

Every response `renderNotifications` writes carries `Cache-Control: no-store` and `Pragma:
no-cache`. This matters for the same reason minting renders inline rather than through a redirect (see
below): a code shown once is still in the response *body*, and no-store is what actually keeps a body
out of a shared machine's disk cache and a signed-out browser's Back button, neither of which the URL
staying clean or an access log staying quiet does anything about.

Minting renders the result **in the same response**, not through the redirect-with-notice pattern
every other admin form in this package uses (`formPost`/`redirectTo`, `internal/httpserver/admin_ui.go`).
A minted code is live for ten minutes; putting it in a redirect's query string would leave it sitting
in the URL and any access log for that whole window, which a page showing a person their own
credential should not do. `formPostTo` generalises `formPost` to redirect somewhere other than
`/admin`, which is what `cancelLink` and `unlinkNotifications` use — there is nothing sensitive in
either notice, so the ordinary pattern applies there.

`?notice=` and `?error=` on this page are a **closed set**, unlike `admin_ui.go`'s `Page`, whose
same-named parameters carry whatever text a handler in that file composed and are rendered verbatim.
`notificationsQueryNotice`/`notificationsQueryError` map a handful of recognised values (`unlinked`,
`canceled`, `nothing_to_unlink`, `unlink_failed`, `cancel_failed`, `sign_in_required`) to catalog text
and render nothing for anything else. This page's entire purpose is instructing someone to type a
credential into a chat, which is exactly the shape a phishing link takes — "click here, then send the
code below to the bot" — rendered in this page's own trusted `role="alert"` styling to whoever follows
it while signed in; `cancelLink` and `unlinkNotifications` therefore return one of the recognised keys
above, never catalog text, so nothing free-form ever reaches the query string this page reads back.

A lookup failure and "not linked" look the same through `Recipient == nil` alone, which is the wrong
thing to tell someone who is actually linked but whose lookup just failed. `notificationsPage` sets
`Notifications.StatusUnknown` alongside `Error` in that case, and the template renders a third,
distinct message rather than falling back to "not linked". `handleMintLink` checks the same flag
before touching `page.Notice`/`page.Error`: a load failure the page already reported is left alone
by a mint outcome that says nothing about whether that earlier lookup can be trusted, whether the
mint itself then succeeds or fails.

`cancelLink` calls `DeleteLinkFlowsForSubject(session.Subject)` and is offered regardless of whether
the caller is currently linked. It is the only way to invalidate a code once minted: `createLinkFlow`
already retires a subject's outstanding code the moment a new one is drawn, but that is no help if a
code was pasted into the wrong window and nobody wants a replacement yet — until now there was no way
to kill it before its ten-minute TTL ran out.

`unlinkNotifications` deletes exactly the row `GetRecipientBySubject(session.Subject)` resolves,
then best-effort tells that conversation it was unlinked via the bot client — the same
fire-and-forget shape `notifyConversationDisplaced` already uses for a displaced link, logged rather
than surfaced on failure since the unlink itself has already committed. Naming another row's id in
the form, or on the query string, reaches nothing: nothing here reads one.

[`manifest/`](../manifest/) holds the Teams app package -- `manifest.json` plus two icons -- that
an operator uploads to Teams admin center so the bot can be installed at all. It is packaging
metadata for the Teams catalog, not something `internal/config` reads at startup; see
[`manifest/README.md`](../manifest/README.md) for what to replace before packaging.
