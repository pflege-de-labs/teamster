---
title: Authenticate a webhook sender
weight: 13
---

Give each sender its own token, scoped to the webhook it uses, and find out why a sender is
refused.

Both `/webhook/alertmanager` and `/webhook/universal` take a token as
`Authorization: Bearer <token>`. The [Teams V2 webhook](../teams-v2-webhook/) is different: its
token is part of the URL.

## Issue a token

Anyone who may use a webhook can issue a token for it: editors and admins for both webhooks, anyone
else for the webhooks granted to them. Granting webhook access is covered in
[Manage roles and access](../roles/#let-someone-use-the-webhooks).

{{% steps %}}

### Create it

Open **/admin/tokens**. Name the token after the sender, for example `alertmanager-prod`, and tick
the webhooks it may send to. Tick only the one the sender uses.

### Copy it

The token, starting with `tst_`, is shown once. Teamster stores only a digest, so it cannot show
it again. Put it in the sender's secret store.

### Configure the sender

See [Send alerts from Alertmanager](../alertmanager/) or
[Send events with the universal webhook](../universal-webhook/).

{{% /steps %}}

You see and revoke the tokens you created. Webhook admins and admins see and revoke everyone's. The
page lists when each token was last used, to within an hour.

Scripts can do the same through the API, with a session or the local admin credentials:

```bash
# issue; the answer carries the token once, in "token"
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/tokens \
  -d '{"name": "alertmanager-prod", "scope": ["alertmanager"]}'

# list
curl -u <admin-user>:<admin-password> http://localhost:8080/api/tokens

# revoke
curl -u <admin-user>:<admin-password> -X DELETE http://localhost:8080/api/tokens/<id>
```

`scope` takes `alertmanager`, `universal` or both.

### A token answers to its creator

Every use checks two things: the token's scope must name the webhook, and its creator, with the
roles and groups of their last sign-in, must still be allowed to use it. Disabling the creator,
removing them from a group or taking away their webhook level revokes their tokens at the next
request. Issue tokens as someone whose access outlives the sender, or the sender stops when they
leave. See [Ownership and sharing](../../concepts/ownership/) and
[ADR 0077](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0077-scoped-tokens-answer-to-their-creator.md).

Tokens issued before release 0.11.0 are unscoped: they reach both webhooks, whoever made them.
Issue new ones to bind them.

## Let a token name people

A message that names people, in `recipients` on the universal webhook or in the label
`teamster_recipient` on either webhook, needs a token that may name them. See
[Send messages to individual people](../direct-messages/). Choose the token's message level under
**May name recipients** when you issue it, or as `messages` in the API:

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/tokens \
  -d '{"name": "password-expiry", "scope": ["universal"], "messages": "anyone"}'
```

| Level | `messages` | The token may name |
| --- | --- | --- |
| nobody | absent | Nobody. |
| only me | `self` | Only its creator. |
| anyone | `anyone` | Anyone in the tenant. |

* **Only me** is open to everyone who may issue a token. Every address in the message must be the
  creator: their Entra object id from their last sign-in (`auth.object-id-claim`), or the one their
  linked chat recorded. If Teamster knows neither, the token names nobody: sign in once, or link
  your chat.
* **Anyone** needs the permission to message anyone, which admins hold and grant; see
  [Let someone message people](../roles/#let-someone-message-people).
* A token's level is never higher than its creator's. Asking for more is refused, with `403` from
  the API.
* Every use checks the creator's level as it is now. An **anyone** token whose creator lost that
  permission names only its creator from then on.
* Tokens from before release 0.11.0 and `webhook.token` have no creator, so they cannot name
  anyone. Issue a token with a message level for such a sender.

The `403` comes before anything is delivered. A refused Alertmanager batch delivers none of its
alerts, including the ones that name nobody. A message that names people is checked even when the
route it matches delivers to a channel. See
[ADR 0082](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0082-naming-people-takes-permission.md)
for the design.

## Use a deployment-wide token

`webhook.token` is one token from configuration, accepted alongside the issued ones. Use it when a
sender has to be configured declaratively, before anyone can sign in to issue a token.

```yaml
webhook:
  token: <token>
```

Deliver it as `TEAMSTER_WEBHOOK_TOKEN` rather than in the file. It is unscoped: it reaches both
webhooks. It cannot name people, so a sender that sets `recipients` or `teamster_recipient` needs
an issued token. Rotate it by changing the value and restarting.

{{< callout type="warning" >}}
The `X-Teamster-Token: <token>` header from earlier releases still works for either kind of token,
but is deprecated and will be removed in a breaking release. Move senders to
`Authorization: Bearer`.
{{< /callout >}}

## When a sender is refused

**`401`**, which Alertmanager logs as `unexpected status code 401`, means Teamster did not accept
the token. The server log says why, in a warning such as
`msg="webhook refused" source=alertmanager reason=…`, and the refusal is counted in
`teamster.webhook.receipts` with state `refused`. Check, in order:

1. The sender sets an `Authorization: Bearer` header. `basic_auth` is not accepted.
2. The token exists on this installation. Issued tokens live in the database, so
   `teamster export` and `import` do not carry them, and neither does a fresh database.
3. The token was not revoked.
4. `webhook.token` is not set in a config file as well as in `TEAMSTER_WEBHOOK_TOKEN`. The file
   wins, so the Secret's value is ignored. Remove the key from the file.
5. There is a token at all. With `webhook.token` unset and none issued, every sender is refused,
   and Teamster logs this at startup.

**`403` with a JSON body** `{"error": "the token's scope, or its creator, does not allow this
webhook"}` means a scoped token was used for a webhook its scope does not name, or its creator may
no longer use it. The `webhook refused` log line names the token and its creator. The refusal is
counted with state `forbidden`.

**`403`** with `{"error": "this token may not name recipients: …"}` means the message names people
and its token may not: it has no message level, it is `webhook.token` or from before 0.11.0, or its
creator may no longer message people. `{"error": "this token may only name its creator as a
recipient"}` means an **only me** token named someone else, or Teamster does not know its
creator's object id yet. Both are logged and counted like the scope refusal. See
[Let a token name people](#let-a-token-name-people).

**`403 RBAC: access denied`** is not Teamster's answer. It is Envoy's wording: a service mesh, such
as an Istio `AuthorizationPolicy`, or a gateway refused the request before it reached the pod. Allow
the sender's workload to reach the webhook path there.

**`503`** means Teamster could not check the token because the database did not answer. The sender
should retry, as it does for any `5xx`.

## What the tokens protect

The webhook endpoints are authenticated by their tokens and nothing else. Whoever holds one can do
more than file a spurious alert.

Labels and annotations are interpolated into templates, and the result reaches a channel or a
person's chat. The sanitizer removes scripts, images and every attribute except a link's `href`,
and keeps an `href` only for `http`, `https` and `mailto`. It does not remove links. An annotation
such as `[Open the runbook](https://evil.example/login)` renders as a link whose text says one thing
and whose target is another, delivered by a service your people trust, at three in the morning.
Links stay because templates link to runbooks and dashboards, so the token is the control:

* **Treat a token as a credential.** Revoke issued tokens at **/admin/tokens**.
* **Keep the webhooks off the internet** if only in-cluster senders need them. Alertmanager in the
  same cluster needs no ingress.
* **Give each sender its own token**, scoped to the one webhook it uses, so one can be revoked
  without breaking the others. A token still reaches every route behind its webhook, so senders in
  different trust boundaries need separate deployments.
* **Terminate TLS in front of Teamster.** The token travels in a header on every request.
* **Alert on refusals.** A token being guessed shows up as `teamster.webhook.receipts` with state
  `refused`:

  ```promql
  sum by (source) (rate(teamster_webhook_receipts_total{state="refused"}[5m])) > 0
  ```

See [Monitor Teamster](../observability/) to enable metrics, and
[ADR 0044](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0044-webhook-access-tokens.md)
for the design.
