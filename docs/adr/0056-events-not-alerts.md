# 0056. Model what arrives as an event, not an alert

* Status: Accepted; superseded in part by [0084](0084-an-alertmanager-notification-is-one-event.md):
  an Alertmanager notification is one event, not one per alert
* Date: 2026-09-30

## Context

Every webhook turned what it received into `models.Alert`, which was Prometheus Alertmanager's
alert with a few fields added. [ADR 0035](0035-a-message-without-a-status-is-delivered-once.md)
let a message without a status through, but kept the names and the shape. The type said
"alert" while carrying deploy notices, build results and Teams V2 posts, and its fields were
Alertmanager's:

* `Status` meant something only as `firing` or `resolved`.
* `StartsAt`, `EndsAt` and `Generator` had no meaning for most senders, but they fed the
  fingerprint.
* `Annotations` was the only free-text field.
* The group an Alertmanager notification arrived in was dropped, so a template could not show
  the group's receiver, key or common labels.

The same shape showed up in the template API (`.Alert.*`), the universal webhook's wire format,
three tables, a metric and the editor's completions. Nobody runs Teamster yet, so this is the
cheapest point at which the model can change.

## Decision

We will replace `models.Alert` with `models.Event`. The event has a core that every webhook fills
and a typed extension for each webhook that knows more.

* **The core** is `Source`, `Key`, `State`, `Labels` and the direct content `Title`, `Text` and
  `Card` ([ADR 0036](0036-direct-content-when-a-route-has-no-template.md)). No adapter fills the
  direct content from anything else.
* **`State`** is `open`, `closed` or empty. Empty is the one-shot delivery from ADR 0035. Open and
  closed opt into the claim protocol. Alertmanager's `firing` and `resolved` map to open and
  closed; anything else it sends is delivered once. On `/webhook/universal`, any other state is
  refused with 400, so a sender that still posts `firing` finds out instead of silently losing
  its updates.
* **`Key`** replaces the fingerprint. When a sender gives none, it is derived from the source, the
  extension's URL and time, and the labels, in the order the fingerprint used. A derived key
  therefore does not change.
* **Extensions** are nil for every other source:
  * `.Event.Alertmanager` carries the annotations, `StartsAt`, `EndsAt`, `GeneratorURL`, and the
    group's receiver, group key, group labels, common labels, common annotations and external URL.
  * `.Event.Universal` carries `Attributes` (the free text that was annotations), `Time` and `URL`.
  * Teams V2 needs none: its message maps onto the core, and its raw body stays in `.Payload`.
* **A template that handles one webhook** reads that webhook's extension directly. A template for
  any webhook reads an extension only inside `{{ with … }}`, because a nil extension fails the
  render. Presets, snippets and the default title follow this rule.
* **Everything alert-named follows:**

  | Before | After |
  | --- | --- |
  | `active_alerts`, `active_alert_recipients` | `active_events`, `active_event_recipients` |
  | `alert_samples` | `event_samples` |
  | `fingerprint`, `status` columns | `event_key`, `state` columns |
  | sample kind `annotation` | sample kind `attribute` |
  | metric `teamster.active_alerts` | metric `teamster.active_events` |
  | receipts attribute `status` | receipts attribute `state` |

* **Existing data is converted.** One migration renames the tables and columns and maps the state
  values. It also rewrites stored templates from `.Alert` to `.Event`. The old
  `Annotations`/`Generator`/`StartsAt` fields go to the universal extension in a template that
  handles only the universal webhook, and to the Alertmanager extension in any other. Down
  reverses it.

Alternatives considered:

* **Keep `Alert` and add fields**, which is ADR 0035's position. It leaves every non-Alertmanager
  sender speaking a vocabulary that is not theirs.
* **A flat generic core** with `Attributes`, `Time` and `URL` for every source. It is simpler for
  templates, but it puts one webhook's concepts on all of them and still has no place for
  Alertmanager's group fields.
* **Accept `.Alert` as an alias during a transition.** Compatibility code for installations that
  do not exist.

This supersedes ADR 0035's decision to keep the alert names. Its one-shot semantics stand.

## Consequences

* A sender that has no alert lifecycle describes its message in its own terms, and Alertmanager's
  group context is available to templates for the first time.
* A template for any webhook has to use `with` to read an extension. A template that doesn't
  renders for its own webhook and fails for others. That was already true of any field the event
  did not carry, but a nil extension makes it an error, not an empty value.
* **This change breaks the previous release, deliberately.** It is a one-off exception to the rule
  in AGENTS.md that a schema change must leave the previous release able to run: the tables,
  their columns, the template API and the wire format change together. A two-release rename would
  add dual-name code for installations that do not exist.
* The universal webhook's payload changes incompatibly: `status`, `fingerprint`, `annotations`,
  `starts_at`, `ends_at` and `generator` become `state`, `key`, `attributes`, `time` and `url`, and
  `ends_at` goes away. Dashboards reading `teamster_active_alerts` have to read
  `teamster_active_events`.
* The template rewrite is textual. It does not catch unusual spacing inside a comparison such as
  `eq  .Alert.Status "firing"`: the field is renamed, but the literal is not. It also cannot tell
  which webhook a template for any webhook was written for, so it assumes Alertmanager. The
  preview shows what such a template renders.
