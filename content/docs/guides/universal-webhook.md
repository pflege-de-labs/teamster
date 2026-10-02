---
title: Send events with the universal webhook
weight: 11
---

Use the universal webhook for any sender that is not Alertmanager: a CI job, a cron script, a
monitoring tool with a generic webhook. The sender posts labels to route on and attributes to
render, and Teamster does the rest.

The endpoint is `POST /webhook/universal`, authenticated with `Authorization: Bearer <token>`.
Issue a token at **/admin/tokens** and, under **May send to**, leave only `/webhook/universal`
ticked; see
[Authenticate a webhook sender](../webhook-tokens/).

## Send a one-off message

A message with no `state` is delivered once and never tracked. It needs only `labels` and
`attributes`:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  -d '{"labels": {"app": "checkout", "environment": "production"},
       "attributes": {"summary": "Deployment finished"}}' \
  http://localhost:8080/webhook/universal
```

Routes select on `labels`. Templates read `attributes` as `.Event.Universal.Attributes`.

{{< callout type="warning" >}}
A message without a `state` that the sender retries after a `502` is delivered again to every
target, because nothing identifies it as the same message. Use a `state` and a `key` when a
duplicate matters.
{{< /callout >}}

## Track an event that opens and closes

Set `state` to opt into the lifecycle Alertmanager alerts have:

1. Post with `"state": "open"` and a stable `key`. Teamster posts a card.
2. Post again with the same `key` and `"state": "open"`. Teamster edits the card in place.
3. Post with the same `key` and `"state": "closed"`. Teamster closes it.

```json
{
  "key": "checkout-latency",
  "state": "open",
  "labels": {"alertname": "HighLatency", "severity": "critical"},
  "attributes": {"summary": "p99 latency above 2s"},
  "time": "2025-12-07T20:07:00Z",
  "url": "https://grafana.example/d/checkout"
}
```

Without a `key`, Teamster derives one from the webhook, `url`, `time` and the sorted labels. Any
`state` other than `open`, `closed` or absent is refused with `400`. See
[Alert lifecycle](../../concepts/alert-lifecycle/) for what happens to a card in each state.

## Send the message content directly

A sender that already knows what to say can skip writing a template. Add any of these fields:

| Field | Sent as |
| --- | --- |
| `title` | The activity feed preview line |
| `text` | Message text, Markdown, sanitized like a template's |
| `card` | An Adaptive Card JSON object |

They are used only when the matching route has no template. A route with a template renders
through it and ignores the three fields. A message with no template and none of the three fields
still goes out with Teamster's built-in message: a title from `summary` or `alertname`, the state
and description, and the event as a JSON block.

Every message sent without a template is followed by a small card saying so. Set
`server.external-url` (`TEAMSTER_SERVER_EXTERNAL_URL`) to the admin UI's address, and that card
links to where templates are created.

## Check the answer

| Status | Means |
| --- | --- |
| `200 {"status":"ok"}` | Delivered. |
| `200 {"status":"partial", …}` | Delivered to some people, not all. See [Send messages to individual people](../direct-messages/). |
| `400` | Invalid JSON, an unknown `state`, or more recipients than `webhook.max-recipients`. Fix the request. |
| `401`, `403` | The token was refused. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `422` | The message addressed people and none could be reached. |
| `502` | Something that may recover failed: Teams, Graph, the database. Retry. |
| `503` | The token could not be checked. Retry. |

A `5xx` body carries a `request_id`. Search the server log for it to find the cause.

## Samples

The repository has a sample for each shape:
[`universal-message.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-message.json),
[`universal-open.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-open.json),
[`universal-closed.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-closed.json)
and
[`universal-direct-message.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/universal-direct-message.json).
Every field is listed in [Webhook payloads](../../reference/webhook-payloads/).

## Migrate a sender written before 0.9.0

Release 0.9.0 renamed the payload fields. The old names are gone, not aliased. Rename them:

| Old | New |
| --- | --- |
| `status: firing` | `state: open` |
| `status: resolved` | `state: closed` |
| `fingerprint` | `key` |
| `annotations` | `attributes` |
| `starts_at` | `time` |
| `generator` | `url` |
| `ends_at` | drop it |

See [ADR 0056](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0056-events-not-alerts.md).
