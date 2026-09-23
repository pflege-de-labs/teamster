# 0041. Complete templates and routes from labels sampled off incoming alerts

* Status: Accepted
* Date: 2026-09-23

## Context

Milestone 9 named "an empty textarea with no hint of what an alert offers" as one of the awkward
parts of writing a template, and [ADR 0014](0014-card-editor.md) answered the rest of that list
without answering this one. A template author has to know that the data is `.Alert.Labels`, which
label keys the senders actually use, and which Adaptive Card properties an element takes. A route
author has to know the same label keys, and their values, to write a selector at all. None of that
is visible in the admin UI; all of it is in the alerts the service already receives.

The constraints are the roadmap's: one binary, no CDN, vendored browser code pinned in
`manifest.json` ([ADR 0031](0031-vendored-browser-libraries-pinned-and-verified.md)), no bundler and
no `package.json`. Logic lives in Go where there is a choice. The textareas must keep working
without JavaScript, and the preview, the palette and the routing check read them directly. A card
template is Go template text that renders into JSON, so `{{ range }}` legitimately appears outside a
JSON string. Deployments run on SQLite alone or as several replicas on one Postgres.

## Decision

**We will upgrade the template, selector and route-check fields to CodeMirror 6 editors.**
`web/editor.js` is an ES module that finds fields marked `data-editor`, imports CodeMirror through an
import map in the layout, and puts an editor after each field. The field stays in the form, hidden,
and is written on every change with an `input` event dispatched, so posting, `preview.js` and
`routing.js` read it as before; the palette asks the editor to insert at its cursor through a
cancelable `teamster:insert` event and falls back to the field when nothing answers. Without
JavaScript, or when anything in `editor.js` throws, the plain field is what remains.

CodeMirror publishes each package as one ES module, `dist/index.js`, so each one is one
`manifest.json` entry verified exactly as `d3` and `adaptivecards` are — fourteen entries, the
packages and their dependencies, about 1.1 MB unminified and fetched only on a page with an editor.
Monaco was the alternative. It ships as a directory tree of several megabytes of AMD modules,
workers and CSS that the one-file-per-package manifest cannot describe without vendoring a build
tool's output, and its JSON mode runs a validating language service that reports every
`{{ range }}` in a card as an error.

**Go template actions are parsed on their own and overlaid on JSON, not linted as JSON.** A small
hand-written parser splits the text into literal text and `{{ … }}` actions and tokenizes inside the
actions; `parseMixed` lays the JSON parser over the text alone. Actions get their own highlighting
and completion, the JSON around them highlights as JSON, and no linter is configured, so nothing
reports a template as invalid. A Lezer grammar would be the conventional way to write the parser
and needs a generator this repository does not have.

**Template completion comes from Go.** `templates.EditorVocabulary` walks `RenderData` by
reflection and lists the functions `funcs` registers beside text/template's own, and the admin page
embeds it as JSON. A field or function added in Go is offered without anyone editing the browser
code.

**Adaptive Card completion comes from the renderer already vendored.** `adaptivecards` 3.0.6 exposes
its element and action registries on `AdaptiveCards.GlobalRegistry`, and every object's
`getSchema()` lists its properties with their enum values. `editor.js` reads that at page load, so
completion offers exactly what the preview renders and follows the library when Renovate bumps it.
Two short lists stay in `editor.js`: the collection properties the library parses by hand rather
than declaring (`body`, `items`, `columns`, `actions`, …) and the objects whose type is implied by
where they sit (`facts` hold a `Fact`). Deriving a schema from `internal/cards` was the fallback,
and would have offered only the handful of elements our own snippets use.

**Label keys, label values and annotation keys are sampled off incoming alerts into a table.**
`processAlert` — the one place Alertmanager and universal alerts meet — hands each alert's labels
and annotation keys to `internal/samples`, before routing, so an alert no route matches yet still
teaches the editor the labels a route for it would need. A Teams V2 post carries no labels and is
never routed, so it is not sampled. `alert_samples` holds one row per kind, key and value with a
count and when it was first and last seen; `GET /api/samples` serves it to the editor once per page
load.

* **Annotation values are never stored.** They are free text — descriptions, summaries, runbook
  prose — high in cardinality and the likeliest place for detail nobody asked this service to keep.
  The sampler drops them before anything is queued; an annotation row's value is always empty.
* **Everything is capped.** A label value longer than `samples.max-value-length` is not sampled.
  Pruning keeps the `samples.max-values-per-key` most recently seen values of each key and deletes
  anything not seen for `samples.retention`, and `/api/samples` applies the same per-key cap and a
  hard row limit, since pruning runs only hourly.
* **Only editors see it.** Label values say what runs where. `/api/samples` requires edit on
  templates or on routes, and the layout only tells the editor where to fetch it when the viewer
  has one of those.

**The part delegated to a library is the write coalescing.** A label every alert carries would
otherwise be an upsert per alert. The sampler keeps a `hashicorp/golang-lru/v2` cache of recently
written tuples and writes one only when it is new or its last write is `samples.flush-interval`
old, carrying the count accumulated in between; an evicted tuple's pending count joins the next
batch, and the counts still pending on shutdown are written before the store closes. Counts are
therefore approximate, which is all completion needs.

**Sampling is asynchronous.** `Observe` copies the keys and does a non-blocking send into a bounded
queue; one worker goroutine, started by `ServeCmd` and stopped after the HTTP drain, owns the cache,
writes each batch in one transaction and runs the prune. A synchronous best-effort write would be
simpler, but even a coalesced write lands on the delivery path of whichever alert happens to be
due, and on SQLite that write queues behind the single connection every delivery uses. A full queue
drops the observation and counts the drop: sampling must never slow delivery, and losing an
observation costs nothing but a slightly lower count.

**Several replicas share the table without coordinating.** The upsert adds counts rather than
replacing them and only ever moves `last_seen` forward, so replicas writing the same tuple each
contribute what they saw. Each replica has its own cache, so a tuple is written up to once per
flush interval per replica. Pruning is a pair of set deletes, idempotent in the way the session
sweep already is, so every replica runs it hourly and the second run finds nothing to do.

Alternatives for the sample store:

* **An embedded Prometheus TSDB.** Built for label indexes, but it is a heavy dependency with a
  write-ahead log and compaction on local disk — per replica, so replicas would each see only their
  own alerts — and it models labels, not annotations.
* **An embedded search index such as Bleve.** Prefix search over a few thousand short strings is
  far below what it is for, and it is again per-replica local state.
* **Ask the sender's Prometheus for label values.** Only Alertmanager has one behind it, the
  universal and Teams V2 senders do not, and annotations are not labels there either.
* **Keep the samples in memory only.** Every restart and every replica would start from nothing,
  and a quiet environment might never show the editor a label it has.

## Consequences

The template and route editors complete what alerts actually carry, the fields of the data a
template receives, template functions and actions, and Adaptive Card element types, properties and
enum values. A route selector completes label keys and then that key's recent values; so does the
routing page's check.

The schema gains `alert_samples` (SQLite `0013`, Postgres `0010`), a new table and nothing else, so
the previous release runs against it and ignores it. Samples begin with the first alert after the
upgrade; there is no backfill.

`samples.enabled` is on by default. Turning it off stops sampling, pruning and serving; rows an
earlier run kept stay in the table, unread, until sampling is enabled again or they are deleted by
hand.

A vendored CodeMirror bump is fourteen Renovate pull requests rather than one, each finished with
`make vendor-record`. `vendor_test.go` now reads the import map as well as `src` attributes, and
checks that every bare import in a vendored module resolves, so a bump that adds a dependency fails
in CI rather than in a browser.

`editor.js` is the first ES module in `web/` and the first script with logic worth testing that the
Go tests cannot reach. The repository has no JavaScript test setup, so the file is kept defensive —
any failure leaves the plain field — and was exercised in a headless browser rather than by a
suite. A test runner is the follow-up if it grows.

Samples are not scoped by grants. An editor whose role is granted only one Team still completes
label values from alerts routed anywhere — host, service or namespace names from other Teams'
alerts. Scoping them would mean recording which route or destination each sample arrived for, and
filtering by the caller's grants at read time; that is deferred until a deployment needs grants to
hide label values, not only destinations.

Using the samples for the template preview — a "sampled alert" built from the most common value of
each label — is not part of this change; the preview's fixed alerts are unchanged.
