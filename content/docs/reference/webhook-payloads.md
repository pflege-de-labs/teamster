---
title: Webhook payloads
weight: 4
---

The endpoints that receive messages, how they authenticate, the bodies they accept and what they
answer.

## Endpoints

| Method and path | Authentication | Body | Routed |
| --- | --- | --- | --- |
| `POST /webhook/alertmanager` | `Authorization: Bearer <token>` | [Alertmanager](#alertmanager) | yes |
| `POST /webhook/universal` | `Authorization: Bearer <token>` | [Universal](#universal) | yes |
| `GET /webhook/broadcasts/{id}` | `Authorization: Bearer <token>` | — | no, see [Broadcast status](#broadcast-status) |
| `POST /teamsv2/{team}/{channel}/{token}` | The token in the path | [Teams V2](#teams-v2) | no, posts to the endpoint's destination |
| `POST /bot/messages` | Bot Framework signed token | Bot Framework activity | — |
| `GET /healthz` | none | — | — |
| `GET /readyz` | none | — | — |

`/bot/messages` is registered only when the bot is configured. Microsoft calls it; no sender
should. Its body is limited to 256 KiB.

`/healthz` answers `200 ok` while the process runs. `/readyz` answers `200 ok`, or `503` with
`shutting down` or `database unreachable`. Both accept `GET` and `HEAD`.

Every response carries an `X-Request-ID` header. The same id is logged as `request_id`.

## Authentication

`/webhook/alertmanager`, `/webhook/universal` and `/webhook/broadcasts/{id}` accept either kind of
token.

| Token | Where it comes from | Scope |
| --- | --- | --- |
| Issued access token, `tst_` and 64 hex digits | `/admin/tokens` or `POST /api/tokens` | The webhooks it names, and only while its creator may still use them. Names people as its message level and its creator allow. Unscoped if issued before scopes existed, and then names nobody. |
| `webhook.token` | Configuration, `TEAMSTER_WEBHOOK_TOKEN` | Both webhooks. Names nobody. |

| Header | Status |
| --- | --- |
| `Authorization: Bearer <token>` | Current. The scheme is case-insensitive. |
| `X-Teamster-Token: <token>` | Deprecated. Read only when there is no `Authorization: Bearer` header. |

Basic auth is not accepted. With no `webhook.token` and no issued token, every request is refused.

A Teams V2 endpoint is authenticated by the 64-hex-digit token in its URL alone. Only a digest of
it is stored.

## Alertmanager

The [Alertmanager webhook payload](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config),
version 4. One notification becomes one event, however many alerts it groups. These fields are
read; others are ignored.

| Field | Becomes |
| --- | --- |
| `groupKey` | `.Event.Key`, as a SHA-256 of `alertmanager` and the group key, and `.Event.Alertmanager.GroupKey` |
| `status` | `.Event.State`: `firing` is `open`, `resolved` is `closed`, anything else is empty |
| `commonLabels` | `.Event.Labels`, which routes select on, and `.Event.Alertmanager.CommonLabels` |
| `receiver` | `.Event.Alertmanager.Receiver` |
| `groupLabels` | `.Event.Alertmanager.GroupLabels` |
| `commonAnnotations` | `.Event.Alertmanager.CommonAnnotations` |
| `externalURL` | `.Event.Alertmanager.ExternalURL` |
| `alerts` | `.Event.Alertmanager.Alerts` |
| `alerts[0].annotations`, `startsAt`, `endsAt`, `generatorURL` | `.Event.Alertmanager.Annotations`, `StartsAt`, `EndsAt`, `GeneratorURL`, only when `alerts` holds one alert |

When a field is missing:

| Missing | Teamster uses |
| --- | --- |
| `groupKey` | A derived key, see [Keys](#keys). |
| `status` | `firing` if any alert fires, otherwise `resolved` unless an alert reports another status. |
| `commonLabels` | The labels all alerts share. |
| `alerts`, or an empty list | Nothing: the request is answered `200` and nothing is delivered. |

A `teamster_recipient` label that the alerts do not all share is not in `commonLabels`. Teamster
then joins the `teamster_recipient` values of all alerts into the event's label, so an addressed
route reaches everyone the group names.

Once the notification is delivered, Teamster also closes, for each alert reported as `resolved`,
the card a release up to 0.11.0 posted for that alert alone. A failure to close one is logged and
does not fail the request.

## Universal

A JSON object. Every field is optional.

| Field | Type | Meaning |
| --- | --- | --- |
| `key` | string | Identifies the event across posts. Derived when absent, see [Keys](#keys). |
| `state` | string | `open`, `closed`, or absent. Anything else is refused with `400`. |
| `labels` | object of strings | What routes select on. |
| `attributes` | object of strings | Free text for templates. Only the keys are sampled. |
| `time` | string, RFC 3339 | When the event started. |
| `url` | string | Where the event came from. |
| `recipients` | array of strings | People an addressed route delivers to: UPNs, mail addresses or Entra object ids. |
| `broadcast` | boolean | `true` sends to everyone the bot can reach, in the background. Refused with `400` together with `recipients`, `teamster_recipient` or `state`. See [Send a message to everyone](../../guides/broadcasts/). |
| `title` | string | Sent as-is when the route has no template. |
| `text` | string, Markdown | Sent, sanitized, when the route has no template. |
| `card` | object, Adaptive Card | Sent as-is when the route has no template. |

| `state` | Delivery |
| --- | --- |
| `open` | Tracked. A repeat with the same `key` updates the message it sent. |
| `closed` | Closes what an `open` with the same `key` sent. Needs no recipients. |
| absent | Delivered once and not tracked. A retry delivers again. |

```json
{
  "key": "optional-stable-id",
  "state": "open",
  "labels": {"alertname": "HighCPU", "severity": "critical"},
  "attributes": {"summary": "CPU spiking"},
  "time": "2025-12-07T20:07:00Z",
  "url": "https://grafana.example/d/cpu",
  "recipients": ["alex.example@example.com"]
}
```

Payloads from before events replaced alerts used `status`, `fingerprint`, `annotations`,
`starts_at`, `ends_at` and `generator`. They are no longer read.

## Labels Teamster reads

| Label | Set by | Meaning |
| --- | --- | --- |
| `teamster_source` | Teamster | The webhook the event arrived at: `alertmanager`, `universal` or `teamsv2`. A sender's own value is overwritten. |
| `teamster_recipient` | The sender | People an addressed route delivers to, comma-separated. Used only when the payload has no `recipients`. |

Recipients are trimmed and deduplicated ignoring case. A message may name at most
`webhook.max-recipients` people.

A message that names people needs a token whose message level allows it, whatever route it
matches. The whole request is checked before anything is delivered. See
[Let a token name people](../../guides/webhook-tokens/#let-a-token-name-people). A broadcast needs
the message level `everyone`.

## Keys

When an event has no key, Teamster derives one as a SHA-256 over:

| Source | Hashed |
| --- | --- |
| Alertmanager | `alertmanager`, `generatorURL` and `startsAt` of a group of one alert, the event's labels sorted by key |
| Universal | `universal`, `url`, `time`, the labels sorted by key, and the recipients when there are any |

## Responses

For `/webhook/alertmanager` and `/webhook/universal`. Error bodies are
`{"error": "<message>"}`.

| Status | Body | Means |
| --- | --- | --- |
| `200` | `{"status": "ok"}` | Every delivery succeeded. |
| `200` | `{"status": "partial", "delivered": <n>, "undelivered": [...]}` | Some people cannot be reached, and a retry will not change that. |
| `202` | `{"status": "accepted", "broadcast": {...}, "status_url": "/webhook/broadcasts/<id>", "delivered": <n>}` | Universal only: a broadcast was queued. `broadcast` is its [status](#broadcast-status); `delivered` counts the channels and linked chats reached during the request, and `undelivered` follows when some were not. |
| `400` | `{"error": "invalid JSON"}` | The body is not the expected JSON, including a malformed time. |
| `400` | `{"error": "unknown state …"}` | Universal only: `state` is not `open`, `closed` or absent. |
| `400` | `{"error": "the message names more recipients than webhook.max-recipients allows: …"}` | Too many recipients. |
| `400` | `{"error": "a broadcast goes to everyone: drop recipients and the teamster_recipient label"}` | A broadcast that also names people. |
| `400` | `{"error": "a broadcast is delivered once: drop state"}` | A broadcast with a `state`. |
| `401` | empty, with `WWW-Authenticate: Bearer realm="webhook"` | No token, or a token Teamster does not know. |
| `403` | `{"error": "the token's scope, or its creator, does not allow this webhook"}` | A scoped token used for a webhook it does not name, or whose creator may no longer use it. |
| `403` | `{"error": "this token may not name recipients: …"}` | The message names people and the token may not: no message level, `webhook.token`, a token from before 0.11.0, or a creator who may no longer message people. Nothing was delivered. |
| `403` | `{"error": "this token may only name its creator as a recipient"}` | An **only me** token named someone else, or its creator's object id is not known. Nothing was delivered. |
| `403` | `{"error": "this token may not broadcast: it needs the message level everyone"}` | A broadcast from a token below `everyone`, or whose creator no longer holds that level. Nothing was delivered. |
| `405` | empty | Not a `POST`. |
| `422` | `{"status": "undelivered", "delivered": 0, "undelivered": [...]}` | Nobody could be reached, or a broadcast matched no route that delivers to people. |
| `502` | `{"error": "bad gateway", "request_id": "<id>"}` | Something that may come right failed: no route, a template, the database, Teams. Retry. |
| `503` | `{"error": "cannot check the token right now"}` | The database did not answer the token lookup. Retry. |

A `502` caused by a missing bot names it instead of `bad gateway`.

Each entry of `undelivered`:

| Field | Holds |
| --- | --- |
| `recipient` | The address as the sender gave it. Empty for `no-recipient`. |
| `reason` | One of the reasons below. |

| Reason | Means |
| --- | --- |
| `invalid-address` | The address is not a UPN, mail address or object id. |
| `unknown-recipient` | Nobody in the directory has that address. |
| `ambiguous-address` | More than one user carries that mail address or alias. |
| `ineligible` | Not an enabled member of the tenant: a guest, a disabled account, or someone who left. |
| `not-installed` | The Teams app is not installed for this person. |
| `no-recipient` | The route delivers to people and the message named none. |
| `blocked` | The person blocked or removed the bot. |
| `no-addressed-route` | A broadcast matched no route that delivers to people. Nothing was sent. |

## Broadcast status

`GET /webhook/broadcasts/{id}` answers a token of the broadcast's creator with the universal webhook
in its scope. `GET /api/broadcasts` returns a list of the same objects to a signed-in user: their
own, and everyone's for admins.

| Field | Holds |
| --- | --- |
| `id` | The broadcast's id. |
| `requested_by` | The token creator's subject. |
| `token_name` | The token it was sent with. |
| `requested_at` | When it was accepted. |
| `state` | `requested`, `running`, `done` or `failed`. |
| `started_at`, `heartbeat_at`, `finished_at` | When it started, last recorded progress and finished. Absent until set. |
| `total` | Everyone it reaches, counted when the run starts. |
| `delivered` | People it reached. |
| `unreachable` | People who blocked or removed the bot. |
| `failed` | People a send failed for. Not retried. |
| `last_error` | Why a `failed` broadcast stopped. Absent otherwise. |

| Status | Means |
| --- | --- |
| `200` | The broadcast. |
| `401` | No token, or a token Teamster does not know. |
| `403` | The token's scope, or its creator, does not allow the universal webhook. |
| `404` | No such broadcast, or it belongs to another creator. Finished broadcasts are kept for 30 days. |
| `405` | Not a `GET`. |

## Teams V2

The bodies a Microsoft Teams Workflows webhook accepts. The shape is recognised from the body.

| Shape | Recognised by | Delivered as |
| --- | --- | --- |
| MessageCard | `"@type": "MessageCard"`, or any of `themeColor`, `sections`, `potentialAction` | One Adaptive Card. Only `OpenUri` actions survive. `themeColor` becomes a container style. |
| Message with attachments | `attachments` | Each attachment of type `application/vnd.microsoft.card.adaptive`, forwarded as is. |
| Text | A non-empty `text` | Text, sanitized. |

```json
{
  "type": "message",
  "attachments": [
    {
      "contentType": "application/vnd.microsoft.card.adaptive",
      "content": {"type": "AdaptiveCard", "version": "1.4", "body": []}
    }
  ]
}
```

| Path segment | Rule |
| --- | --- |
| `{team}`, `{channel}` | Lower case letters, digits and hyphens, 1 to 64 characters, no leading or trailing hyphen. Matched case-insensitively. |
| `{token}` | The endpoint's token, shown once when it is created or rotated. |

Responses. Error bodies are `{"error": "<message>"}`.

| Status | Means |
| --- | --- |
| `200` | Posted. Body `{"status": "ok"}`. |
| `400` | Not JSON, or neither text nor an Adaptive Card. |
| `401` | `invalid token`. |
| `404` | `unknown endpoint`: no endpoint has that team and channel, or the path is incomplete. |
| `405` | Not a `POST`. Carries `Allow: POST`. |
| `413` | `payload too large`: the body is over 128 KiB. |
| `502` | Teams, the database or the endpoint's template failed. |

Nothing is tracked. A Teams V2 message is never updated or closed.

## See also

* [Send alerts from Alertmanager](../../guides/alertmanager/)
* [Send events with the universal webhook](../../guides/universal-webhook/)
* [Move a sender off a Teams Workflows webhook](../../guides/teams-v2-webhook/)
* [Authenticate a webhook sender](../../guides/webhook-tokens/)
* [Template data](../template-data/)
