---
title: Template data
weight: 3
---

What a template can read and call. Templates use Go's
[`text/template`](https://pkg.go.dev/text/template) syntax.

## Template parts

A template has three parts. Each is rendered against the same data.

| Part | Rendered as | Notes |
| --- | --- | --- |
| Title | Plain text | Whitespace, line breaks included, collapses to single spaces. Shown as the activity feed preview. |
| Text | Markdown, then sanitized HTML | Raw HTML passes through Markdown and is then sanitized. |
| Card | Adaptive Card JSON | Output must be valid JSON. Output that is empty or only whitespace sends no card. |

A template needs at least one part. A template with a card and no title gets this title:

```gotemplate
{{ $summary := "" }}{{ with .Event.Alertmanager }}{{ $summary = default (index .Annotations "summary") (index .CommonAnnotations "summary") }}{{ end }}{{ with .Event.Universal }}{{ $summary = index .Attributes "summary" }}{{ end }}{{ default $summary (default .Event.Labels.alertname "Update") }}
```

## Top-level fields

| Field | Type | Holds |
| --- | --- | --- |
| `.Event` | object | The normalized event, see [Event](#event). |
| `.Now` | string | Render time in UTC, RFC 3339, such as `2025-12-07T20:07:00Z`. |
| `.Payload` | any | The request body as decoded JSON. Teams V2 webhook only; nil for every other webhook. |
| `.Recipient` | object | The person a chat message is rendered for, see [Recipient](#recipient). Empty for a channel. |

## Event

Every event has this core, whichever webhook it arrived at.

| Field | Type | Holds |
| --- | --- | --- |
| `.Event.Source` | string | `alertmanager`, `universal` or `teamsv2`. |
| `.Event.Key` | string | What identifies the event across posts. Derived when the sender gives none. Empty for Teams V2. |
| `.Event.State` | string | `open`, `closed`, or empty for a message delivered once. |
| `.Event.Labels` | map of string | What routes select on, `teamster_source` included. |
| `.Event.Title` | string | Direct title, when the sender supplied one. |
| `.Event.Text` | string | Direct text, when the sender supplied it. |
| `.Event.Card` | JSON | Direct Adaptive Card, when the sender supplied one. For Teams V2, the first card of the message. |
| `.Event.Alertmanager` | object or nil | Alertmanager extension, see below. Nil for every other webhook. |
| `.Event.Universal` | object or nil | Universal webhook extension, see below. Nil for every other webhook. |

### Alertmanager extension

Set for an event from `POST /webhook/alertmanager`. One notification is one event, however many
alerts it groups.

| Field | Type | From the payload |
| --- | --- | --- |
| `.Event.Alertmanager.Alerts` | list of alerts | `alerts`, in the order sent, see below |
| `.Event.Alertmanager.Annotations` | map of string | `alerts[0].annotations`, for a group of one alert only |
| `.Event.Alertmanager.StartsAt` | time | `alerts[0].startsAt`, for a group of one alert only |
| `.Event.Alertmanager.EndsAt` | time | `alerts[0].endsAt`, for a group of one alert only |
| `.Event.Alertmanager.GeneratorURL` | string | `alerts[0].generatorURL`, for a group of one alert only |
| `.Event.Alertmanager.Receiver` | string | `receiver` |
| `.Event.Alertmanager.GroupKey` | string | `groupKey` |
| `.Event.Alertmanager.GroupLabels` | map of string | `groupLabels` |
| `.Event.Alertmanager.CommonLabels` | map of string | `commonLabels` |
| `.Event.Alertmanager.CommonAnnotations` | map of string | `commonAnnotations` |
| `.Event.Alertmanager.ExternalURL` | string | `externalURL` |

For a group of several alerts, `Annotations`, `StartsAt`, `EndsAt` and `GeneratorURL` are empty.
`len .Event.Alertmanager.Alerts` tells the two cases apart.

Each entry of `.Event.Alertmanager.Alerts`:

| Field | Type | From the payload |
| --- | --- | --- |
| `.Status` | string | `alerts[].status`: `firing` or `resolved` |
| `.Labels` | map of string | `alerts[].labels` |
| `.Annotations` | map of string | `alerts[].annotations` |
| `.StartsAt` | time | `alerts[].startsAt` |
| `.EndsAt` | time | `alerts[].endsAt` |
| `.GeneratorURL` | string | `alerts[].generatorURL` |
| `.Fingerprint` | string | `alerts[].fingerprint` |

The core fields come from the group: `.Event.Key` is a hash of `groupKey`, `.Event.Labels` comes from
`commonLabels`, and `.Event.State` from `status` (`firing` is `open`, `resolved` is `closed`,
anything else is empty). See [Alertmanager](../webhook-payloads/#alertmanager) for payloads that
leave these out.

### Universal extension

Set for an event from `POST /webhook/universal`.

| Field | Type | From the payload |
| --- | --- | --- |
| `.Event.Universal.Attributes` | map of string | `attributes` |
| `.Event.Universal.Time` | time | `time` |
| `.Event.Universal.URL` | string | `url` |
| `.Event.Universal.Recipients` | list of string | `recipients` |
| `.Event.Universal.Broadcast` | bool | `broadcast` |

### Teams V2

An event from `POST /teamsv2/…` has no extension.

| Field | Holds |
| --- | --- |
| `.Event.Source` | `teamsv2` |
| `.Event.Labels` | Only `teamster_source`. |
| `.Event.Title`, `.Event.Text`, `.Event.Card` | The parsed message. `.Event.Text` is already HTML. |
| `.Payload` | The body as sent, for fields the parsed form flattens, such as `{{ .Payload.themeColor }}`. |

### Nil extensions

Reading a field of a nil extension fails the render. A template that handles one webhook reads its
extension directly. A template for any webhook wraps each in `with`:

```gotemplate
{{ with .Event.Alertmanager }}{{ .Annotations.summary }}{{ end }}
{{ with .Event.Universal }}{{ .Attributes.summary }}{{ end }}
```

A label key that cannot follow a dot is read with `index`:

```gotemplate
{{ index .Event.Labels "app.kubernetes.io/name" }}
```

## Recipient

Set when a route delivers to a person's chat.

| Field | Holds | Person named in the message | Linked chat |
| --- | --- | --- | --- |
| `.Recipient.ID` | Entra object id | yes | yes |
| `.Recipient.DisplayName` | Display name | yes | yes |
| `.Recipient.GivenName` | Given name | yes | empty |
| `.Recipient.Surname` | Surname | yes | empty |
| `.Recipient.UPN` | User principal name | yes | empty |
| `.Recipient.Mail` | Mail address | yes | empty |

All fields are empty for a channel.

## Functions

| Function | Signature | Returns |
| --- | --- | --- |
| `toJSON` | `toJSON <value>` | `value` encoded as JSON. Fails the render if it cannot be encoded. |
| `default` | `default <value> <fallback>` | `value` when it is a non-empty string, otherwise `fallback` when that is a string, otherwise an empty string. Works on a missing map key. |

The built-in functions of `text/template` are available as well: `and`, `or`, `not`, `len`,
`index`, `slice`, `print`, `printf`, `println`, `html`, `js`, `urlquery`, `call`, and the
comparisons `eq`, `ne`, `lt`, `le`, `gt`, `ge`.

```gotemplate
{{ default .Event.Labels.severity "unknown" }}
{{ toJSON .Event.Labels }}
```

## See also

* [Write templates](../../guides/templates/)
* [Webhook payloads](../webhook-payloads/)
