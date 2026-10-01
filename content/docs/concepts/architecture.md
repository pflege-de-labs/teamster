---
title: Architecture
weight: 4
---

Teamster is a single Go binary with its admin UI embedded. It needs a database and two Entra app
registrations, and nothing else at runtime. This page names the parts an operator deals with. The
developer view, with packages and design decisions, is in the
[architecture document](https://github.com/pflege-de-labs/teamster/blob/main/docs/architecture.md)
on GitHub.

## The parts

```text
 Alertmanager ───► /webhook/alertmanager ─┬─► routing ─► template ─┐
 any sender ─────► /webhook/universal ────┘                        ├─► Teams bot ─► channel / chat
 Power Automate ─► /teamsv2/… ───────── its own channel ─► template ┘        ▲
                                                                             │ /bot/messages
 browser ────────► /admin, /api                                        Microsoft Teams
                      │
                      ├─► database (SQLite or Postgres)
                      └─► Microsoft Graph: Teams, channels, people, app installs
```

| Part | What it does |
| --- | --- |
| Webhooks | accept events from senders that present a token; a Teams V2 endpoint carries its token in the URL and posts to the one channel it names |
| Routing | picks targets and a template from the event's labels |
| Templates | render the title, text and Adaptive Card |
| Teams bot | posts and edits every channel card and chat message, through the Bot Framework |
| Microsoft Graph | lists Teams and channels for the pickers, looks up people, installs the Teams app |
| Database | holds the configuration, the open alerts, tokens, the users who signed in, and the audit trail |
| Admin UI and API | manage templates, destinations, routes, tokens and access |

## Two Entra registrations

Teamster uses two separate credentials, and rotating one never touches the other:

* **The Graph registration** (`graph.*`) reads. Graph does not let an application post to
  channels, so this one never posts.
* **The bot registration** (`bot.*`) posts. Every card goes out through it, which is why nothing
  reaches Teams without the bot, and why the Teams app has to be installed in every team a route
  posts to.

See [Grant the Microsoft Graph permissions](../../guides/graph-permissions/) and
[Set up the Teams bot](../../guides/teams-bot/).

## State

Everything Teamster remembers lives in its database: the configuration, which message belongs to
which open alert, webhook tokens, and the audit trail.

* **SQLite** is a file and needs nothing else, but allows exactly one instance.
* **Postgres** lets several instances share one database behind one Service. There is no queue, no
  leader election and no sharding: any instance can take any request.

Schema migrations run when an instance opens the database, unless `database.migrate` says
otherwise. See [Choose a storage backend](../../guides/storage/).

## Listeners

| Listener | Default | Serves |
| --- | --- | --- |
| `server.addr` | `:8080` | webhooks, `/bot/messages`, the admin UI and API, `/healthz`, `/readyz` |
| `metrics.addr` | `127.0.0.1:9090` | the Prometheus exporter, when `metrics.enabled` is on; unauthenticated |

The webhooks and `/bot/messages` authenticate themselves: a token, or Microsoft's signature on a
bot activity. The admin UI and API need a session or basic auth. You can therefore publish the
webhooks to the internet and keep `/admin` on a private network. See
[Observability](../../guides/observability/) for the metrics listener.

## Related

* [Routing]({{< ref "/docs/concepts/routing" >}}) and
  [Alert lifecycle]({{< ref "/docs/concepts/alert-lifecycle" >}}) explain the middle of the
  diagram.
* [Configuration reference](../../reference/configuration/) lists every key.
