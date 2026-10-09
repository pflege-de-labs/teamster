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

Set when a route delivers to a person's chat. The fields come from the person's Entra profile. A
field Entra does not have for someone is empty.

| Field | Type | Holds |
| --- | --- | --- |
| `.Recipient.ID` | string | Entra object id |
| `.Recipient.DisplayName` | string | Display name |
| `.Recipient.GivenName` | string | Given name |
| `.Recipient.Surname` | string | Surname |
| `.Recipient.UPN` | string | User principal name |
| `.Recipient.Mail` | string | Mail address |
| `.Recipient.JobTitle` | string | Job title |
| `.Recipient.Department` | string | Department |
| `.Recipient.CompanyName` | string | Company name |
| `.Recipient.OfficeLocation` | string | Office location |
| `.Recipient.EmployeeID` | string | Employee id |
| `.Recipient.Address.Street` | string | Street of the business address |
| `.Recipient.Address.PostalCode` | string | Postal code of the business address |
| `.Recipient.Address.City` | string | City of the business address |
| `.Recipient.Address.State` | string | State of the business address |
| `.Recipient.Address.Country` | string | Country of the business address |
| `.Recipient.BusinessPhones` | list of string | Business phone numbers |
| `.Recipient.MobilePhone` | string | Mobile phone number |
| `.Recipient.PreferredLanguage` | string | Preferred language, such as `de-DE` |
| `.Recipient.UsageLocation` | string | Usage location, a country code such as `DE` |

Which fields are filled depends on the chat:

| Chat | Fields |
| --- | --- |
| Person named in the message, or reached by a broadcast through the directory | All |
| Linked chat whose person is in the directory | All |
| Linked chat whose person is not in the directory | Only `ID` and `DisplayName` |
| Channel | None |

After an upgrade from a release without these fields, a person already in the directory gets them
at the next install run (`bot.reconcile-interval`), or when a message looks them up again after
`bot.directory-ttl`. Until then they are empty.

### The event a person sees

When a route delivers to the people a message names, each person's message is rendered from a
copy of the event that names only them:

* `.Event.Universal.Recipients` holds only the addresses that named them.
* The `teamster_recipient` label, in `.Event.Labels`, `.Event.Alertmanager.CommonLabels` and each
  alert's labels, holds only the addresses that named them. Where it named only others, it is
  absent.
* `.Event.Alertmanager.Alerts` holds only the alerts that name them and the alerts that name
  nobody. When one alert is left, `Annotations`, `StartsAt`, `EndsAt` and `GeneratorURL` are filled
  from it, as for a group of one.

A channel, and a route to one linked chat, sees the whole event.

## Functions

| Function | Signature | Returns |
| --- | --- | --- |
| `toJSON` | `toJSON <value>` | `value` encoded as JSON. Fails the render if it cannot be encoded. |
| `default` | `default <value> <fallback>` | `value` when it is a non-empty string, otherwise `fallback` when that is a string, otherwise an empty string. Works on a missing map key. |

The built-in functions of `text/template` are available as well: `and`, `or`, `not`, `len`,
`index`, `slice`, `print`, `printf`, `println`, `html`, `js`, `urlquery`, `call`, and the
comparisons `eq`, `ne`, `lt`, `le`, `gt`, `ge`.

So are the [sprig](https://masterminds.github.io/sprig/) functions — strings, regular expressions,
lists, dicts, semver and more — except these:

| Left out | Why |
| --- | --- |
| `env`, `expandenv` | They would expose Teamster's client secrets and tokens to anyone who may edit a template. |
| `getHostByName` | It reaches the network. |
| `now`, `ago`, `date`, `dateInZone`, `dateModify`, `htmlDate`, `htmlDateInZone`, the `rand…` functions, `uuidv4` | They read the clock or randomness, so a template would not render the same twice. Use `.Now` for the time. |
| `bcrypt`, `htpasswd`, `derivePassword`, `encryptAES`, `decryptAES`, the `gen…` and `buildCustomCert` functions | They cost CPU on every preview and have no use in a message. |

`toJSON` and `default` are Teamster's own and take precedence over sprig's. Mind the argument
order of `default`: the value comes first. The pipeline form from sprig's documentation,
`.x | default "y"`, always yields `"y"`; write `default .x "y"`.

```gotemplate
{{ default .Event.Labels.severity "unknown" }}
{{ toJSON .Event.Labels }}
{{ regexFind "^[a-z]+" .Event.Universal.Attributes.commit_message }}
{{ splitList "\n" .Event.Universal.Attributes.commit_message | first | trunc 80 }}
```

## See also

* [Write templates](../../guides/templates/)
* [Webhook payloads](../webhook-payloads/)
