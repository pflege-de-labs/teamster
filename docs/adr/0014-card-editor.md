# 0014. Improve editing the card, rather than building an editor

* Status: Accepted
* Date: 2026-09-11

## Context

[The roadmap](../roadmap.md) left this open deliberately: once the preview from milestone 1.4
existed, we would know whether editing JSON beside a live preview was already enough, and if not,
whether to reach for Microsoft's Adaptive Cards Designer or build a form-based editor over the card
elements this service uses.

With the preview in hand, what is actually awkward about writing a template is not the JSON. It is:

* an empty textarea with no indication of what an alert offers or what a card looks like
* remembering the syntax for a FactSet, or for an action, well enough to type it from memory
* clicking Preview after every change
* one quotation mark in an alert's summary breaking the card, which is invisible until it happens

None of those is solved by a visual designer, and two of them are made worse by one.

## Decision

We will not build a form-based editor, and we will not embed the Adaptive Cards Designer.

The Designer is several megabytes, and a template here is not a card: it is a Go template that
*renders into* a card. `{{ if eq .Alert.Status "firing" }}` is not valid JSON, so a WYSIWYG editor
would either refuse to load half the templates this service already runs, or quietly discard the
templating on save. A form-based editor has the same problem in a smaller way, and buys an operator
a worse version of the JSON they can already see.

Instead the JSON editing gets what it was missing:

* **A palette** of the card elements this service's own templates use — text, facts, columns, a link
  action, all labels, and a conditional block — inserted at the cursor, with the comma added when
  the insertion point needs one.
* **A starter card**, so a new template begins as a complete, rendering example to edit down rather
  than an empty box.
* **Preview as you type**, debounced, with the existing Preview button and the sample selector
  unchanged. The render stays on the server, so what is previewed is what will be sent.

**The fragments live in Go**, in `internal/cards`, not in the page. A test renders every snippet and
the starter against a sample alert *and* against an empty one, so a palette that inserts something
the renderer rejects cannot ship. That is the same reason the preview renders server-side: one
implementation of templating, not two.

The snippets also teach a quoting idiom: every value that comes from an alert is written as
`{{ toJSON .Alert.… }}` **without** surrounding quotes, because `toJSON` emits the quotes and escapes
what is inside them. A summary containing a quotation mark then produces a card rather than broken
JSON. Writing the snippets is what surfaced this — three of the first six were wrong in exactly that
way, and the tests caught them.

## Consequences

Milestone 9 is much smaller than its placeholder assumed, and the roadmap says so rather than
leaving a promise of an editor nobody is building.

`templates.default` now takes `any` for its fallback as well as its value: a missing key of a nil
map arrives as an invalid value, and `default .Annotations.summary .Labels.alertname` puts one in
the fallback position. That was a live bug for any template using the two-alert-field form, and the
starter card is exactly that form.

An operator who wants a visual designer still does not have one. If that turns out to be the thing
people ask for, this ADR is the place to record why it was not built, and a later one can supersede
it — with the round-trip problem as the thing to solve first.
