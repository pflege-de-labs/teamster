# 0104. The Alertmanager receiver is a label

* Status: Accepted
* Date: 2026-11-10

## Context

Teams has many channels and Alertmanager has many receivers. An installation that already chooses
the channel in its Alertmanager route tree has these choices:

* **Regex matchers, first-match order, mute times and inhibition.** Teamster cannot express all of
  them. Its selectors are exact, and every matching root delivers.
* **Keep the route tree** and make every receiver post to `/webhook/alertmanager`. Teamster routes
  on `commonLabels` only and does not see which receiver sent a group, so the channel the tree
  picked is lost.

## Decision

We will set the label `teamster_receiver` on every Alertmanager event, from the payload's
`receiver`. One teamster route per receiver then picks the channel the tree chose.

* **Only the last `/`-separated segment is kept.** The Prometheus Operator names each receiver
  `<namespace>/<AlertmanagerConfig>/<name>`, and the namespace differs between clusters that run
  the same configuration. Keeping the prefix would need one route per cluster for the same channel.
* **The server sets it.** For Alertmanager events it overwrites a value in the alert labels; for
  any other source it is removed, as `teamster_team` and `teamster_channel` are for anything but
  Teams V2 ([ADR 0052](0052-the-receiving-webhook-is-a-label.md)).
* **An empty `receiver` sets no label**, rather than one that every receiverless sender shares.
* **It is added after the event key**, like `teamster_source`. A key derived from labels, for a
  payload without `groupKey`, stays what it was, so cards posted before the upgrade are still found.

## Consequences

* A route can select on `{"teamster_receiver": "infra"}`, and selectors that do not name it are
  unaffected.
* Two receivers with the same last segment in different `AlertmanagerConfig`s are
  indistinguishable. Name them apart when that matters.
* A receiver name containing `/` is cut at its last one.
