---
title: Move a sender off a Teams Workflows webhook
weight: 15
---

Replace a Microsoft Teams "Workflows" (Power Automate) webhook URL with a Teamster one. Teamster
accepts the payloads that webhook accepts, so moving a sender means changing one URL.

The endpoint is `POST /teamsv2/{team}/{channel}/{token}`. There is no header: the URL is the
credential, as it was before.

## Move the sender

{{% steps %}}

### Create the destination

The channel the sender posts into must exist as a destination in Teamster. Create it under
**Destinations** on **/admin** if it does not.

### Create the endpoint

Under **Teams V2 webhooks** on **/admin**, pick the destination and give the URL two readable
segments for the team and the channel. Segments are lower case letters, digits and hyphens, up to
64 characters, starting and ending with a letter or digit.

The page that answers the form shows the full URL, token included, once. Teamster stores only a
digest of the token and cannot show it again.

### Point the sender at it

Replace the old Workflows URL in the sender's configuration with the new one, for example
`https://teamster.example/teamsv2/platform/alerts/<token>`. Keep it as secret as the old one.

### Test it

```bash
curl -H 'Content-Type: application/json' -d '{"text": "something broke on node-3"}' \
  https://teamster.example/teamsv2/platform/alerts/<token>
```

A `200 {"status":"ok"}` means the message was posted.

{{% /steps %}}

If the URL is lost or leaks, use **New token** on the endpoint. It replaces the token, and the old
URL stops working at once.

## What the sender may post

Teamster recognises the shape from the body:

{{< tabs >}}
{{< tab name="Adaptive Card" >}}

```json
{
  "type": "message",
  "attachments": [
    {
      "contentType": "application/vnd.microsoft.card.adaptive",
      "content": {"type": "AdaptiveCard", "version": "1.4", "body": []}
    }
  ]
}
```

The card is forwarded to Teams byte for byte.

{{< /tab >}}
{{< tab name="Text" >}}

```json
{"text": "something broke on node-3"}
```

The text is sanitized against the same allowlist templates use.

{{< /tab >}}
{{< tab name="MessageCard" >}}

```json
{
  "@type": "MessageCard",
  "themeColor": "D70000",
  "title": "Disk almost full",
  "text": "node-3 is at 94%",
  "sections": [{"facts": [{"name": "severity", "value": "critical"}]}],
  "potentialAction": [
    {"@type": "OpenUri", "name": "Open runbook", "targets": [{"os": "default", "uri": "https://example.test/runbook"}]}
  ]
}
```

The legacy connector card is converted into one Adaptive Card. Two things are lost:

* Actions other than `OpenUri` (`HttpPOST`, `ActionCard`, `InvokeAddInCommand`) are dropped. Each
  needs the connector to call the sender back.
* `themeColor` becomes one of Adaptive Cards' container styles, by hue: red is `attention`, orange
  and yellow `warning`, green `good`, blue and purple `accent`, grey none.

{{< /tab >}}
{{< /tabs >}}

The samples
[`teamsv2-card.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/teamsv2-card.json),
[`teamsv2-text.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/teamsv2-text.json)
and
[`teamsv2-messagecard.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/teamsv2-messagecard.json)
are in the repository.

## Shape the message with a template

By default the payload is posted as it came, followed by a small card saying that no template is
defined. To shape it, pick a **Template** in the endpoint's form. Only templates that handle Teams
V2 payloads are offered; see [Write templates](../templates/).

In such a template:

* `.Event.Title`, `.Event.Text` and `.Event.Card` hold the parsed message. `.Event.Text` is already
  HTML.
* `.Event.Source` is `teamsv2`. There is no `.Event.Alertmanager` or `.Event.Universal`.
* `.Payload` is the body as sent, for fields the parsed form flattens, such as
  `{{ .Payload.themeColor }}` or `{{ range .Payload.sections }}`.

The template preview has a **Teams V2 webhook** sample to try it against.

## What it does not do

* **No routing.** The URL decides the channel, as it did before.
* **No tracking.** The payloads carry no key and no state, so a message is never updated or closed.
  Use the [universal webhook](../universal-webhook/) for that.

## Check the answer

| Status | Means |
| --- | --- |
| `200` | Posted. |
| `400` | The body carries neither text nor a card. |
| `401` | Wrong token. |
| `404` | No endpoint is configured for that team and channel. |
| `413` | The body is over 128 KiB. |
| `502` | Teams or the database failed, or the endpoint's template did not render. |
