---
title: Send messages to individual people
weight: 12
---

Send a message to the people it names, each in their own Teams chat, instead of to a channel. A
password-expiry notice or a ticket assigned to someone are typical uses.

You need the Teams bot configured, because only the bot can write to a person's chat. See
[Set up the Teams bot](../teams-bot/). With `bot.global-install` on, Teamster can install the bot
for people who do not have it yet.

## Address people

{{% steps %}}

### Create a route that addresses people

In the admin UI, create or edit a route and set **Delivers to** to **People named in the
message**. Give it a label selector for the messages it should take, for example
`{"kind": "password-expiry"}`, and a template.

Each person gets their own message, rendered for them, so the template can greet them:

```gotemplate
Hello {{ .Recipient.GivenName }}, your password expires on {{ .Event.Universal.Attributes.expires }}.
```

`.Recipient` is described in [Template data](../../reference/template-data/).

### Name the people in the message

On the [universal webhook](../universal-webhook/), list them in a top-level `recipients` field:
UPNs, mail addresses or Entra object ids.

```json
{
  "key": "password-expiry-2026-10-07",
  "labels": {"kind": "password-expiry"},
  "recipients": ["alice@example.com", "bob@example.com"],
  "attributes": {"expires": "2026-10-07", "reset_url": "https://passwords.example.com/reset"},
  "time": "2026-09-30T08:00:00Z"
}
```

A sender with no such field, such as [Alertmanager](../alertmanager/), sets the label
`teamster_recipient` instead, with several addresses separated by commas. When a message has both,
the `recipients` list wins. Addresses are trimmed, and duplicates are dropped ignoring case.

### Read the answer

| Answer | Means |
| --- | --- |
| `200 {"status":"ok"}` | Everyone was reached. |
| `200 {"status":"partial", "delivered": n, "undelivered": [...]}` | Some people cannot be reached, and a retry will not change that. |
| `422 {"status":"undelivered", ...}` | Nobody could be reached. Alertmanager does not retry a `4xx`. |
| `400` | The message names more than `webhook.max-recipients` people. Nothing was sent. |
| `502` | Something that may recover failed. Retry. |

Each entry in `undelivered` has the `recipient` as given and a `reason`:

| Reason | Means |
| --- | --- |
| `invalid-address` | Not an object id, UPN or mail address. |
| `unknown-recipient` | No such person in the directory. |
| `ineligible` | Not an enabled member of the tenant: a guest, a disabled account, someone who left. |
| `not-installed` | The bot's Teams app is not installed for this person. |
| `no-recipient` | The route addresses people and the message named none. |
| `blocked` | The person blocked or removed the bot. |

{{% /steps %}}

## Update or close the messages

Use `"state": "open"` and a `key` to get the tracked lifecycle:

* A repeated `open` with the same `key` edits each person's message in place.
* A `closed` post with the same `key` sends each person a new message saying it cleared. It does
  not need to repeat the recipients. A new message rather than an edit, because Teams does not
  notify on an edit.

A message with no `key` gets one derived from its labels, time, url and recipients. A message with
no `state` is delivered once, and is delivered again to everybody if the sender retries it.

## Tune the limits

| Key | Environment variable | Default | Effect |
| --- | --- | --- | --- |
| `webhook.max-recipients` | `TEAMSTER_WEBHOOK_MAX_RECIPIENTS` | `100` | Most people one message may name. Above that it is refused with `400`. |
| `webhook.fanout-concurrency` | `TEAMSTER_WEBHOOK_FANOUT_CONCURRENCY` | `8` | How many people one message is sent to at once. |
| `bot.directory-ttl` | `TEAMSTER_BOT_DIRECTORY_TTL` | `24h` | How long a looked-up person is trusted before Graph is asked again. |
| `bot.inline-install-budget` | `TEAMSTER_BOT_INLINE_INSTALL_BUDGET` | `5` | With `bot.global-install`, how many people one message may install the app for. The rest wait for the next install run. |

```yaml
webhook:
  max-recipients: 100
  fanout-concurrency: 8
bot:
  directory-ttl: 24h
  inline-install-budget: 5
```

One request's fan-out has to finish within `server.write-timeout`. Raise it if you raise the
limits.

See [ADR 0063](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0063-a-message-names-its-recipients.md)
for the design.
