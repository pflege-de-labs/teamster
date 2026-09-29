# 0052. The receiving webhook is a label

* Status: Accepted
* Date: 2026-09-29

## Context

Alerts arrive at three webhooks:

* `/webhook/alertmanager`
* `/webhook/universal`
* the Teams V2 compatible `/teamsv2/…` endpoints ([ADR 0030](0030-teams-v2-compatible-webhooks.md))

`models.Alert.Source` records which one, but routing only sees labels. A route could not send
Alertmanager alerts one way and generic webhook messages another. Nor could a template tell from
the labels which payload shape it was rendering.

## Decision

We will set the label `teamster_source` on every incoming alert, with the value `alertmanager`,
`universal` or `teamsv2`.

* **The server sets it and overwrites any value the sender supplied**, so a route can trust it.
* **It is added after the fingerprint is computed.** A universal payload without a fingerprint gets
  one hashed from its labels. Adding the label first would give an alert posted before the upgrade a
  new fingerprint, and it would never resolve.
* **It is added before sampling**, so the route editor completes it like any other label
  ([ADR 0041](0041-editor-completion-from-sampled-labels.md)).
* **Teams V2 messages stay unrouted.** The URL still picks the channel, and the label is only there
  for the endpoint's template to read.
* The preview samples carry the label too.

The prefixed name was chosen over `source` or `webhook`, which alert rules already use often enough
that overwriting them would break someone's routing.

## Consequences

* A route can select on `teamster_source`, and exact-match selectors that do not name it are
  unaffected.
* A sender's own `teamster_source` label is lost, by design.
* No schema change.
