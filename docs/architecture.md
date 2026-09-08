# Architecture

Teamster is a single Go binary that accepts alert webhooks, picks a Teams channel and an
Adaptive Card template per alert, and posts or updates the card through the Microsoft Graph API.
State lives in a local SQLite file; there are no other runtime dependencies.

## Components

| Package | Responsibility |
| --- | --- |
| `cmd/server` | Entry point. Installs the signal handler and hands the resulting context to the command tree. |
| `internal/cli` | kong command tree and its wiring. `ServeCmd` is the default command: it opens the store, constructs the Graph client, serves HTTP and shuts down on cancellation. |
| `internal/config` | Configuration schema (kong tags), XDG config file search paths, validation. |
| `internal/httpserver` | HTTP routing, webhook handlers, alert processing, admin JSON API, basic auth and request logging middleware. |
| `internal/routing` | Selects a route for an alert's labels. |
| `internal/templates` | Renders an Adaptive Card from a Go template plus alert data. |
| `internal/graph` | Microsoft Graph client: OAuth2 client credentials, post and update channel messages. |
| `internal/store` | `Store` interface and its SQLite implementation for templates, destinations, routes and active alerts. |
| `internal/models` | Shared data types: `Alert`, `Route`, `Template`, `Destination`, `ActiveAlert` and the two webhook payload shapes. |
| `internal/httpserver/web` | Static assets for the admin UI. |

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

## Deployment

The container image is built multi-stage: `golang:1.24` compiles a static binary with
`CGO_ENABLED=0`, and the runtime stage is `gcr.io/distroless/static-debian12:nonroot` running as
uid 65532. Configuration is mounted at `/etc/xdg/teamster/config.yaml` — the system-wide XDG path
the binary already searches — and the SQLite file lives in the `/data` volume. See
[ADR 0004](adr/0004-container-image.md).

Images are published to `ghcr.io/<owner>/<repo>` for `linux/amd64` and `linux/arm64`. Every build
is tagged with its short commit sha, branches and pull requests get moving tags, and a release
retags the existing commit image instead of rebuilding it — same digest, same bytes.
`:latest` only ever moves forward, to the newest stable release. See
[ADR 0005](adr/0005-image-tagging-and-promotion.md).

## Request flow

```
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

## Routing rules

`routing.SelectRoute` sorts routes by descending priority, breaking ties on name, and returns the
first non-default route whose label selector matches the alert exactly — every selector key must
be present with the same value. An empty selector never matches. If nothing matches, the first
route flagged as default wins; with no default configured the alert is an error.

## Alert lifecycle

Timestamp columns are declared `DATETIME`; the SQLite driver only converts them back to
`time.Time` for that declared type. `NewSQLiteStore` refuses to open a database whose timestamp
columns are declared otherwise, since every read from it would fail.

`active_alerts` is keyed by fingerprint and stores the Team, channel and Graph message ID. A
repeated `firing` alert therefore edits the existing card instead of posting a new one, and a
`resolved` alert edits the card one last time before the row is deleted.

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
