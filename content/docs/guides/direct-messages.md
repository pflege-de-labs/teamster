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

In the admin UI, create or edit a route and set **Delivers to** to **Any person named in
the message**. Give it a label selector for the messages it should take, for example
`{"kind": "password-expiry"}`, and a template. Anyone who may edit routes may create one.

Keep the selector narrow. The route takes every message it matches, from any sender, and renders
it with your template. Whom it reaches is bounded by each sender's token, set in the next step.

Each person gets their own message, rendered for them, so the template can greet them:

```gotemplate
Hello {{ .Recipient.GivenName }}, your password expires on {{ .Event.Universal.Attributes.expires }}.
```

`.Recipient` is described in [Template data](../../reference/template-data/).

### Issue a token that may name people

A message that names anyone is refused with `403` unless its token may name them. At
**/admin/tokens**, set **May name recipients** when you issue the sender's token:

| Choice | API value | The token may name |
| --- | --- | --- |
| nobody | absent | Nobody. Every message that names people is refused. |
| only me | `self` | Only you, the token's creator. |
| anyone | `anyone` | Anyone in the tenant. |
| anyone, and broadcast | `everyone` | Anyone, and [everyone at once](../broadcasts/). |

**Anyone** is offered only if an admin let you message anyone; see
[Let someone message people](../roles/#let-someone-message-people). A password-expiry sender
needs it. **Only me** suits a script that notifies its own author. See
[Let a token name people](../webhook-tokens/#let-a-token-name-people) for the details.

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
`teamster_recipient` instead, with several addresses separated by commas. For an Alertmanager group,
the label's values on all its alerts are joined. When a message has both, the `recipients` list
wins. Addresses are trimmed, and duplicates are dropped ignoring case.

In an Alertmanager alerting rule, set the label from a label the alert already carries, or name
a fixed person:

```yaml
groups:
  - name: certificates
    rules:
      - alert: CertificateExpiring
        expr: cert_expiry_seconds < 7 * 86400
        labels:
          kind: certificate-expiry
          teamster_recipient: "{{ $labels.owner_email }}"
      - alert: BackupFailed
        expr: backup_last_success_age_seconds > 86400
        labels:
          kind: backup
          teamster_recipient: "alice@example.com,bob@example.com"
```

Each alert in a group is addressed by its own label, so one notification can reach different
people for different alerts. See [How addresses are matched](#how-addresses-are-matched) for
what each address may be.

### Read the answer

| Answer | Means |
| --- | --- |
| `200 {"status":"ok"}` | Everyone was reached. |
| `200 {"status":"partial", "delivered": n, "undelivered": [...]}` | Some people cannot be reached, and a retry will not change that. |
| `422 {"status":"undelivered", ...}` | Nobody could be reached. Alertmanager does not retry a `4xx`. |
| `400` | The message names more than `webhook.max-recipients` people. Nothing was sent. |
| `403` | The token may not name these people. Nothing was sent. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `502` | Something that may recover failed. Retry. |

Each entry in `undelivered` has the `recipient` as given and a `reason`:

| Reason | Means |
| --- | --- |
| `invalid-address` | Not an object id, UPN or mail address. |
| `unknown-recipient` | No such person in the directory. |
| `ambiguous-address` | More than one person carries this mail address. Use their UPN or object id. |
| `ineligible` | Not an enabled member of the tenant: a guest, a disabled account, someone who left. |
| `not-installed` | The bot's Teams app is not installed for this person. |
| `no-recipient` | The route addresses people and the message named none. |
| `blocked` | The person blocked or removed the bot. |

{{% /steps %}}

## How addresses are matched

Each address is one of three forms. Matching ignores case.

| Form | Example | Matches |
| --- | --- | --- |
| Entra object id | `0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0` | The user with that id. Only a GUID counts as an id. |
| User principal name | `alice@example.com` | The user whose UPN it is. |
| Mail address | `a.smith@example.com` | The user whose primary mail address, or any SMTP alias, it is. Used when no UPN matches. |

Anything without an `@` that is not a GUID is answered as `invalid-address`.

* **Where Teamster looks.** First in its own directory of people, which a previous lookup or the
  [install run](../teams-bot/#install-the-bot-for-everyone) filled. An entry older than
  `bot.directory-ttl` (default `24h`) is looked up in Microsoft Graph again. Aliases are found only
  through Graph, so the first message to an alias costs a Graph call.
* **A typo is remembered.** An address Graph does not know is answered `unknown-recipient`
  without asking Graph again for 10 minutes. A person just added in Entra can take that long to
  become reachable under an address that failed before.
* **Who is reached.** Enabled members of the tenant only. Guests, disabled accounts and people
  who left are answered `ineligible`.
* **An address two people share.** A mail address or alias carried by more than one user names
  nobody for certain. It is answered `ambiguous-address`: name the person by UPN or object id
  instead.
* **Only me.** A token limited to its creator may use any of the three forms, as long as the
  address resolves to the creator's own object id.

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
limits. Every send also waits its turn in the bot's pacing, and a throttled one waits for its
`Retry-After`. A send that would wait past the deadline fails, and the sender gets a `502` and
retries. See [Pace the calls to Teams](../teams-bot/#pace-the-calls-to-teams).

See [ADR 0063](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0063-a-message-names-its-recipients.md)
for the design.
