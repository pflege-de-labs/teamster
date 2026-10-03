---
title: Send a message to everyone
weight: 13
---

Send one message to everyone the bot can reach, each in their own Teams chat, without listing
them. An office closure or a company-wide notice are typical uses.

You need the Teams bot configured. Everyone is:

* every enabled member of the tenant with the app installed, from the directory that
  `bot.global-install` keeps, and
* everyone who linked their chat and is not in that directory,

each once. Without `bot.global-install`, that is only the people who linked a chat. People who
blocked the bot are still sent to; if the send fails, they are counted as unreachable.

## Send a broadcast

{{% steps %}}

### Get the level everyone

Broadcasting takes the message level **anyone, and broadcast to everyone** (`everyone` in the
API), which includes naming anyone. Admins hold it.
An admin grants it to others under **Who may message people** on **/admin/access**; see
[Let someone message people](../roles/#let-someone-message-people).

### Issue a token that may broadcast

At **/admin/tokens**, issue a token for the universal webhook and set **May name recipients** to
**anyone, and broadcast**. In the API, that is `"messages": "everyone"`:

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/tokens \
  -d '{"name": "office-notices", "scope": ["universal"], "messages": "everyone"}'
```

Like every token, it broadcasts only while its creator still holds the level. See
[Let a token name people](../webhook-tokens/#let-a-token-name-people).

### Create a route that addresses people

Create or edit a route that selects the message, for example `{"kind": "office-notice"}`, and set
**Delivers to** to **People named in the message**. Everyone gets their own copy, rendered for
them, so the template can use `.Recipient`. A person in the directory has their whole Entra
profile there; someone who only linked a chat has just `ID` and `DisplayName`. See
[Template data](../../reference/template-data/#recipient).

A broadcast is routed like any message. Channels and linked chats it matches get it during the
request. Only the routes that deliver to **People named in the message** send it to everyone.

### Send it

Post to the [universal webhook](../universal-webhook/) with `"broadcast": true`:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  -d '{"labels": {"kind": "office-notice"}, "text": "The office is closed on Friday.", "broadcast": true}' \
  http://localhost:8080/webhook/universal
```

The repository has the same as `samples/universal-broadcast.json`. A broadcast may not carry
`recipients`, the `teamster_recipient` label or a `state`: it is delivered once and never updated
or closed.

### Read the answer

| Answer | Means |
| --- | --- |
| `202 {"status":"accepted", "broadcast": {...}, "status_url": "/webhook/broadcasts/<id>", "delivered": n}` | Queued. `delivered` counts the channels and linked chats reached during the request. |
| `400` | The message also names people or sets `state`. Nothing was sent. |
| `403` | The token may not broadcast. `webhook.token` and tokens from before 0.11.0 never may. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `422` with reason `no-addressed-route` | No route that delivers to people matches. Nothing was sent. |
| `502` | Something that may recover failed. Retry. |

{{% /steps %}}

## Follow its progress

Ask the `status_url` with a token of the same creator:

```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/webhook/broadcasts/<id>
```

`state` is `requested` while it waits, `running` while it is sent, then `done` or `failed`.
`total`, `delivered`, `unreachable` (blocked or removed the bot) and `failed` count people. A token
of anyone else gets `404`.

**Broadcasts** in the admin UI (**/admin/broadcasts**) lists your own broadcasts, and everyone's
for admins. It appears when the bot is configured and you may broadcast. `GET /api/broadcasts`
returns the same list. Finished broadcasts are kept for 30 days.

## Know what to expect

* Every replica with a bot looks for waiting broadcasts and sends one at a time, 25 people per
  step, `webhook.fanout-concurrency` at once.
* If a replica stops, another takes the broadcast over within about two minutes, from the last
  step it recorded. Up to 25 people may get it twice.
* A failed send is counted, not retried.
* The audience is read when the broadcast starts. People who join later get it only if a replica
  takes the broadcast over and reads them after the point it got to.

See [ADR 0083](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0083-broadcasts-run-in-the-background.md)
for the design.
