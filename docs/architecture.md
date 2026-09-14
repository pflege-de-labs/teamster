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
| `internal/graph` | Microsoft Graph client: OAuth2 client credentials, post and update channel messages. |
| `internal/store` | `Store` interface, its SQLite and Postgres backends sharing one adapter; `internal/store/migrations` owns the schema for templates, destinations, routes and active alerts. |
| `internal/models` | Shared data types: `Alert`, `Route`, `Template`, `Destination`, `ActiveAlert` and the two webhook payload shapes. |
| `internal/httpserver/web` | Embedded static assets: icons, the web manifest and the Tailwind stylesheet built from `views/styles.css`. |

Dependencies are injected through constructors — `store.NewSQLiteStore`, `graph.NewClient`,
`routing.New`, `httpserver.NewServer(cfg, store, graphClient)` — so every component can be
exercised with a substitute in tests. There is no package-level mutable state. `NewServer` takes
the unexported `messenger` interface rather than `*graph.Client`, see
[ADR 0002](adr/0002-messenger-interface.md).

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
                 routing.SelectRoute(alert.Labels)
                                 │
             store.GetTemplate + store.GetDestination
                                 │
                 templates.Render → Adaptive Card JSON
                                 │
              ┌──────────────────┴───────────────────┐
        status=firing                          status=resolved
              │                                      │
   active alert known?                     active alert known?
     yes → graph.UpdateMessage               yes → graph.UpdateMessage
     no  → graph.PostMessage                       + store.DeleteActiveAlert
              │                                 no → no-op
     store.UpsertActiveAlert
```

Any other `status` value is rejected. Handler errors map to `400` for malformed JSON, `401` for a
bad token, and `502` when routing, rendering, the store or Graph fails.

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
and change nothing. The UI hides what a role may not do, which is a courtesy rather than a control.

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

`/admin/routing` draws the path an alert takes and answers which route a set of labels would take.
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

Matching a label set highlights every path that alert takes rather than the winning route alone:
the webhook, each route that delivers, the routes they were reached through, and the channels they
land in are drawn in the match colour while everything else dims — so a fan-out is read off the same
picture.

`POST /api/routing/match` returns the winning route and **why** it won — `selector`, `default`,
`none` or `no-routes`. That reason comes from `routing.Match`, which holds the rule in one place;
`SelectRoute`, used by the delivery path, is a wrapper over it. The browser never re-implements
selector matching, so the answer cannot drift from what actually delivers alerts.

## Messages

A template renders into a title, formatted text and an Adaptive Card, each optional and at least
one required. `templates.RenderMessage` renders all three; the title is collapsed to one line
because that is what the Teams activity feed previews, and an untitled card falls back to
`templates.DefaultTitle`, the summary line this service sent before templates could name their own.

Rendered text is sanitized in `templates.Sanitize` — parsed with `golang.org/x/net/html` and
written back through an allowlist — before it reaches the Graph client, so every caller gets the
same guarantee. `graph.Message` carries the three parts, and the Graph client assembles the body
with an explicit `<attachment id="1">` where the card goes. See
[ADR 0010](adr/0010-message-shape.md).

## Routing rules

Routes form a tree. `routing.Plan` picks a **root** the way routing always worked — descending
priority, ties on name, the first non-default route whose selector matches the alert exactly, every
selector key present with the same value, an empty selector never matching, the default last — and
then walks that root's children.

A child is evaluated only once its parent matched, and applies when its own selector matches. Every
matching child delivers; a greedy one delivers *instead of* its parent, a non-greedy one *as well
as* it. An unset destination or template is inherited from the nearest ancestor that sets one, so
"the same card, one more channel" is a route with a single field. The walk stops at
`routing.MaxDepth`.

`Plan` returns a `Result`: the reason, the root that matched, and one `Delivery` per message with
its destination and template resolved. `routing.ValidateRoute` and `ValidateDelete` are called from
both write paths and keep the tree acyclic, bounded and free of children that could never fire. See
[ADR 0011](adr/0011-nested-routes.md).

## Configuration transfer

`internal/transfer` reads the configuration into a versioned bundle and writes one back. It carries
templates, destinations, routes and grants — no credentials, no sessions, no alert state — and
preserves ids, so a bundle re-imported where it came from changes nothing.

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

Two rules are enforced by the store rather than by the code above it, because a
check and the write it guards are otherwise two steps that another writer can
get between.

A role granted the same scope twice is a duplicate, not a second permission, so
`(role, team_id, channel_id)` is unique and replacing a role's scope is a set
delete rather than a list and a loop. Route writes and deletes run their cycle
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

Delivery is best effort per destination: one channel failing does not cost the others their message,
and the failures are reported together as a `502`. That is safe because each delivery records its
own message id, so a retry updates the cards that made it rather than duplicating them. Resolution
walks the stored cards rather than the plan, so a card in a channel the routes no longer name still
stops claiming the alert is firing.

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

`POST /api/templates/preview` renders a template body against a built-in sample alert through the
same `templates.Render` the delivery path uses, and returns the Adaptive Card JSON. The browser
draws it with the vendored renderer in `web/vendor`. A template that fails to render comes back as
an error field with status 200, because a broken template is the answer the operator asked for.

`GET /api/graph/teams` and `GET /api/graph/teams/{id}/channels` read the tenant's teams and
channels through the Graph client, behind a five minute in-memory cache because the Graph throttles
and those lists barely change. Failures are not cached. The destination form ships its Team and
Channel fields as ordinary text inputs and a script upgrades them to name-based selects once those
endpoints answer, so a missing permission, a throttle or an outage costs the convenience rather
than the ability to configure a destination.

Regenerating the UI needs `make generate`, which runs templ and Tailwind. Their output is
committed, so building the service does not.

## Admin API

Everything outside `/webhook/*` sits behind HTTP basic auth using `admin-username` and
`admin-password`. `/admin` serves the UI; `/api/templates`, `/api/destinations` and `/api/routes`
(plus their `/{id}` variants) provide CRUD over the stored configuration.

## Configuration

Layered, lowest precedence first: kong tag defaults → `TEAMSTER_*` environment variables →
`$XDG_CONFIG_DIRS` files → `$XDG_CONFIG_HOME` file → `./config.yaml` → `--config FILE` →
command line flags. Kong applies resolvers before env-backed defaults, so a config file value
beats the matching environment variable.
See [README](../README.md#configuration) for the concrete paths and
[ADR 0001](adr/0001-kong-xdg-configuration.md) for the reasoning.
