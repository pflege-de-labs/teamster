---
title: Write templates
weight: 15
---

Write a template that turns an event into a Teams message, choose which webhook's events it
handles, and set the default each webhook falls back to.

Templates live under **Templates** on **/admin**. Every field a template can read is listed in
[Template data](../../reference/template-data/).

## Write a template

{{% steps %}}

### Start from a preset

Open a new template and pick one under **Start from a preset**. It fills the name, title, text,
card and webhooks in one go, which is quicker than starting empty.

### Fill in the parts

A template has three optional parts and needs at least one:

| Part | What it is | Where it shows |
| --- | --- | --- |
| Title | A Go template rendering to one line of plain text | The Teams activity feed preview |
| Message text | A Go template rendering to Markdown | The message body |
| Adaptive Card JSON | An Adaptive Card, as a Go template | Below the text |

Write a title when the message is only a card. Otherwise the feed previews it as `Card`; without a
title Teamster falls back to the `summary` annotation or attribute, then `alertname`, then
`Update`. A channel post with a card also shows the title at the top of the card.

A minimal message text for Alertmanager:

```gotemplate
**{{ .Event.Alertmanager.Annotations.summary }}** is {{ .Event.State }}

{{ default .Event.Alertmanager.Annotations.description "No description." }}
```

### Choose the webhooks it handles

Tick **Alertmanager**, **Universal webhook** or **Teams V2 webhook** for the payloads the template
is written for, or leave all unticked for any.

* A Teams V2 endpoint offers only templates that handle Teams V2 payloads.
* A route whose selector pins `teamster_source` offers only templates for that webhook.
* An event that reaches a template not written for its webhook gets its webhook's default template
  instead, or the built-in message when there is none, and a warning is logged.

### Preview it

The preview renders the template against a sample payload for each webhook, and can show the JSON
the bot would send for a channel post and for a chat.

### Attach it

Pick the template on a route, the global default route, or a Teams V2 endpoint. See
[How routing works](../../concepts/routing/).

{{% /steps %}}

## Read webhook-specific fields safely

Fields only one webhook knows live in its extension: `.Event.Alertmanager` or `.Event.Universal`.
The extension is nil for every other webhook, and reaching into a nil one fails to render.

A template that handles one webhook can read its extension directly:

```gotemplate
{{ .Event.Alertmanager.Annotations.summary }}
```

A template for any webhook wraps it in `with`:

```gotemplate
{{ with .Event.Alertmanager }}{{ .Annotations.summary }}{{ end }}
{{ with .Event.Universal }}{{ .Attributes.summary }}{{ end }}
```

A label key that cannot follow a dot needs `index`:

```gotemplate
{{ index .Event.Labels "app.kubernetes.io/name" }}
```

Use `default` for a key an event may not set, and `toJSON` to embed a structure in a card.

## Know what the text may contain

Message text is Markdown. Teamster renders it to HTML and sanitizes it:

* `p`, `br`, `b`, `strong`, `i`, `em`, `u`, `s`, `code`, `pre`, `blockquote`, `ul`, `ol`, `li`,
  `h1` to `h3` and `a` survive.
* `script`, `style` and similar elements are dropped with their contents. Anything else is unwrapped
  to its text.
* A link keeps its `href` only for `http`, `https` and `mailto`. Every other attribute is dropped.

Raw HTML passes through the Markdown parser, so a template written as HTML keeps working. A Team
channel receives the sanitized HTML. A person's chat receives Markdown emitted from that sanitized
HTML.

A card body that renders to nothing sends no card rather than failing, so
`{{ if .Event.Card }}…{{ end }}` is safe for a payload without a card.

{{< callout type="warning" >}}
Annotations and attributes come from whoever can post to the webhook. Sanitizing removes scripts
and images, but not links whose text and target differ. See
[What the tokens protect](../webhook-tokens/#what-the-tokens-protect).
{{< /callout >}}

## Set the default template per webhook

Each webhook has a default template, used when a route or Teams V2 endpoint names no template that
handles its events. Choose them under **Default template per webhook** in the templates panel, or
through the API:

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/templates/source-defaults \
  -d '{"templates": {"alertmanager": "<template id>", "universal": "<template id>", "teamsv2": ""}}'
```

An empty id is the built-in message. Only a template that handles the webhook can be its default.
`GET` on the same path returns the current choice.

On its first start, Teamster stores three presets and makes each the default of its webhook, unless
that webhook already has one:

| Preset | Renders |
| --- | --- |
| Alertmanager (default) | A state-coloured card: summary, severity, description, labels, start and end, and links to the generator URL and a `runbook_url` annotation |
| Universal webhook (default) | The sender's own card or text when it sent one, otherwise a state-coloured card: summary, state, description, labels and the URL |
| Teams V2 webhook (default) | The payload's card, a MessageCard converted to one, or its text alone |

They are ordinary templates. Edit or delete them freely; a deleted preset is not recreated.

The global default route has its own template, chosen on its row in the Routes panel or with
`PUT /api/routes/global-default` and `{"template_id": "…"}`. Without one it sends the built-in
message.

## Use editor completion

The template fields, a route's label selector and the label box on **/admin/routing** complete as
you type. Press `Ctrl-Space` to ask for completion anywhere.

* Inside `{{ … }}` they offer the template data, the functions and the actions (`if`, `range`,
  `end`, …).
* After a label map such as `.Event.Labels.`, or an attribute map such as
  `.Event.Universal.Attributes.`, they offer the keys recent events carried. A key that cannot
  follow a dot is inserted as `(index .Event.Labels "app.kubernetes.io/name")`.
* In the card they offer Adaptive Card element types, property names and enum values.
* A label selector offers label keys, then the recent values of the chosen key.

Without JavaScript the fields are plain text boxes.

The keys and values come from sampling every Alertmanager and universal event, whether or not a
route matches it. Attribute and annotation **values are never stored**, only their keys. Label
values are stored, so only a role that may edit templates or routes can read them, at
`GET /api/samples`.

Sampling is on by default. The values shown are the defaults:

```yaml
samples:
  enabled: true              # sample incoming events at all
  retention: "720h"          # forget a key or value not seen for this long
  max-values-per-key: 50     # keep only the most recently seen values of each label key
  max-value-length: 200      # do not sample a longer label value
  lru-size: 4096             # samples held in memory to coalesce writes
  flush-interval: "5m"       # write a sample already stored at most this often
```

Sampling never delays a delivery: a background writer drops samples when it falls behind, so the
counts are approximate. `enabled: false` (`TEAMSTER_SAMPLES_ENABLED=false`) stops sampling and
completion. Rows kept before stay in the `event_samples` table until deleted.

## Try templates against sample payloads

The repository's
[`samples/`](https://github.com/pflege-de-labs/teamster/tree/main/samples) directory has a payload
for each webhook and state. Post one with `curl` to see the template on a real message:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  --data @samples/alertmanager-firing.json http://localhost:8080/webhook/alertmanager
```

| Sample | Shows |
| --- | --- |
| `alertmanager-firing.json`, `alertmanager-resolved.json` | An Alertmanager alert opening and resolving |
| `universal-open.json`, `universal-closed.json` | A tracked universal event |
| `universal-message.json` | A one-off universal message |
| `universal-direct-message.json` | Direct content, for a route with no template |
| `universal-password-expiry.json` | A message to the people it names |
| `teamsv2-card.json`, `teamsv2-text.json`, `teamsv2-messagecard.json` | The three Teams V2 shapes |

## Migrate templates from before 0.9.0

Templates written against the old `.Alert` data were rewritten by the migration that introduced
events: `.Alert.Status` became `.Event.State`, `.Alert.Fingerprint` became `.Event.Key`, and
`.Alert.Annotations` and its siblings moved into `.Event.Alertmanager`, or `.Event.Universal` for a
template that handles only the universal webhook. Templates you keep outside Teamster need the same
change. See
[ADR 0056](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0056-events-not-alerts.md).
