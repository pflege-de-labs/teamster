---
title: Alert lifecycle
weight: 2
---

Teamster remembers which message it sent for which alert. That is what lets it update a card
while an alert is firing and change it when the alert clears, instead of posting a new card each
time.

## Events and keys

Every webhook is turned into an *event*. An Alertmanager notification is one event, however many
alerts Alertmanager grouped into it, so the group shares one card; a universal post is one event.

An event's **key** says which earlier event it continues:

* Alertmanager: the notification's `groupKey`. Every notification for the group continues the
  same event while alerts join, change and resolve.
* Universal webhook: the payload's `key`. Without one, Teamster derives a key from the source, the
  `url`, the `time`, the labels and the recipients. Send your own `key` if any of those change
  between updates of the same alert.

## State

An event's state decides what happens to it.

| State | Alertmanager | Universal webhook | What Teamster does |
| --- | --- | --- | --- |
| open | `firing`: any alert in the group fires | `"state": "open"` | posts a message, or updates the one it posted for this key |
| closed | `resolved`: every alert has resolved | `"state": "closed"` | updates or follows up the message for this key, then forgets it |
| none | any other status | no `state` | delivers once and tracks nothing |

A universal `state` other than `open` or `closed` is refused with `400`.

## Open: post once, then update

The first open event for a key posts a message to each target its routes name. Teamster stores
the message id per channel and per person. A repeated open event with the same key edits those
messages in place.

## Closed: resolve

A closed event finds the messages stored for its key, not the ones the routes would pick now. A
card still clears after you change the routes it came through.

* **In a channel**, the card is edited one last time to show the resolved state.
* **In a chat**, Teamster sends a new message. Teams does not notify anyone of an edit, so a card
  closed in place would leave the person on call never told that it cleared.

The stored record is then deleted. A closed event for a key with no open message delivers nothing.

{{< callout type="info" >}}
Alertmanager sends resolved notifications only with `send_resolved: true` in its webhook
configuration. Without it, cards stay open. See
[Send alerts from Alertmanager](../../guides/alertmanager/).
{{< /callout >}}

## No state: fire and forget

An event without a state is rendered, routed and delivered once. Nothing is stored, so sending it
again posts a second message. This is the shape for senders without a lifecycle of their own, and
the same contract the Teams V2 webhook has.

## Fan-out and failures

One event can reach several channels and people. Each target is delivered and tracked on its own:

* One target failing does not stop the others. The webhook then answers `502`.
* The sender's retry updates the messages that already arrived rather than duplicating them.
* A person who uninstalled or blocked the bot is counted as `blocked` rather than failed, and is
  not retried for that event. The next event tries again.

## One card, even under concurrent deliveries

Before posting, Teamster claims the right to post the card for that key and channel. If the same
event arrives twice at once, for example at two replicas, one wins and posts. The other gets a
`502`, and its retry edits the card the winner posted.

A claim left behind by a process that died is taken over by the next attempt once
`max(30s, 3 × bot.timeout-sec)` has passed. There is no cleanup job; recovery happens on the next
delivery.

## Related

* Which targets an event reaches: [Routing]({{< ref "/docs/concepts/routing" >}}).
* The payload fields: [Webhook payloads](../../reference/webhook-payloads/).
* Delivery outcomes as metrics: [Metrics](../../reference/metrics/).
