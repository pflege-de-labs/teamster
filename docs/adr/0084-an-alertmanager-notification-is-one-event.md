# 0084. An Alertmanager notification is one event

* Status: Accepted
* Date: 2026-10-02

## Context

Alertmanager groups alerts before it notifies. `group_by`, `group_wait` and `group_interval` are
an operator's decision about what belongs together. The notification carries the group: its
`groupKey`, `groupLabels`, `commonLabels`, `commonAnnotations` and a `status` that is `firing`
while any of its alerts fires, followed by the alerts themselves.

`/webhook/alertmanager` split every notification into one event per alert, keyed by fingerprint
([ADR 0056](0056-events-not-alerts.md)). A group of twenty alerts became twenty cards in the
channel. That undid the grouping the operator had configured, and a template had no way to show
the group as a whole.

## Decision

We will make one Alertmanager notification one event.

* **Key.** The SHA-256 of the source and `groupKey`, so every notification for a group edits the
  same card. A payload without `groupKey` falls back to `deriveKey`.
* **State.** The notification's `status`: `firing` is open, `resolved` closed, anything else
  delivered once. A payload without `status` takes it from its alerts: firing if any alert is.
* **Labels.** `commonLabels`, which is what every alert in the group shares and therefore what a
  route can rely on. For a group of one alert they are that alert's labels, so existing routes
  keep matching. A payload without `commonLabels` gets the labels its alerts share. Recipient
  labels the alerts do not share are joined into `teamster_recipient`, so an addressed route
  ([ADR 0063](0063-a-message-names-its-recipients.md)) still reaches everyone the group names.
* **Template data.** `.Event.Alertmanager.Alerts` is the list as Alertmanager sent it.
  `Annotations`, `StartsAt`, `EndsAt` and `GeneratorURL` stay as a convenience and are filled only
  when the group holds exactly one alert. A template tells the two cases apart with
  `len .Event.Alertmanager.Alerts`. No template function was added for this, because `len` already
  does it.
* **Sampling.** A group of several alerts samples each alert's labels and annotations too. Its
  common labels leave out what the alerts differ in, and that is what an editor wants completed.
* **Transition.** Cards posted per alert by an earlier release are keyed by fingerprint, and no
  group event closes them. Until those cards have aged out, each alert a notification reports as
  `resolved` also closes any card left under its fingerprint, rendered as that alert alone. This
  is best effort: a failure is logged and does not fail the notification, because a route that
  has since gone would otherwise make Alertmanager retry forever.

Alternatives considered:

* **Route on `groupLabels`.** These are only the `group_by` labels. Selectors on anything else,
  such as `severity`, would stop matching.
* **Key the group by receiver as well.** One group sent to two receivers is still the same group,
  and two routes can already fan it out to two channels.
* **Replace the flat fields with an `.Alert` pointer** that is nil unless the group holds one
  alert. This is cleaner, but every stored template would have to be rewritten, and a previous
  release running against the rewritten templates would fail to render them.

This supersedes the part of ADR 0056 that made each alert of a notification its own event. The
rest of ADR 0056 stands.

## Consequences

* A channel shows one card per Alertmanager group, and that card is edited as alerts join, change
  and resolve.
* A template written for one alert keeps working for groups of one. For larger groups, the flat
  fields are empty and the template has to range over `Alerts`. The Alertmanager preset and the
  palette's new alert-list snippet do this. Presets seed only new installations
  ([ADR 0055](0055-each-webhook-has-a-default-template.md)), so an existing installation keeps the
  default template it was seeded with until an admin re-applies the preset.
* After an upgrade, a group that is already firing gets a new group card beside its old per-alert
  cards. The old cards are closed as their alerts resolve.
* There is no schema change, and the previous release runs against this one's data unchanged.
* Closing per-alert cards is transitional code. It should be removed once no installation can
  still hold such a card, and the roadmap tracks that.
