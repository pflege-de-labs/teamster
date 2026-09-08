# Architecture

Teamster is a single Go binary that accepts alert webhooks, picks a Teams channel and an
Adaptive Card template per alert, and posts or updates the card through the Microsoft Graph API.
State lives in a local SQLite file; there are no other runtime dependencies.

## Components

| Package | Responsibility |
| --- | --- |
| `cmd/server` | Entry point. Builds the kong parser, resolves configuration, opens the store, constructs the Graph client and the HTTP server. |
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
exercised with a substitute in tests. There is no package-level mutable state.

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
