# 0030. Accept the payloads a Teams V2 webhook accepts

* Status: Accepted
* Date: 2026-09-16

## Context

Teamster exists to replace the Teams webhooks a team already has. Until now, moving onto it meant
changing the sender: the two ingest endpoints are alert-shaped — a status, a label set, a
fingerprint — and both run what arrives through label routing and a stored Adaptive Card template
before anything reaches a channel.

A sender pointed at a Microsoft Teams "Workflows" (Power Automate) webhook has none of that. It has
already decided what the message says, and the URL it posts to has already decided which channel
the message lands in. There is nothing to route on and no template to render. Asking such a sender
to be rewritten before it can move is the largest cost of adopting this service, and it falls on
whoever owns the sender rather than on whoever chose teamster.

Three request shapes are in the wild, and a migration meets all three: the V2 envelope
(`{"type": "message", "attachments": [...]}`), a bare `{"text": "..."}`, and the MessageCard that
an O365 connector took before connectors were retired.

The constraint that shapes the rest is authentication. A Power Automate webhook URL is the whole
credential — long, opaque and unguessable — and the senders being migrated are exactly the ones that
cannot set a header. `X-Teamster-Token`, which the two existing webhooks use, is not available to
them.

## Decision

We will add `POST /teamsv2/{team}/{channel}/{token}`, which posts what it is given to the channel
the slugs name.

**It runs no routing and renders no template.** The sender decided both. Nothing is written to
`active_alerts` either: there is no fingerprint and no status, so there is nothing later to update
or resolve. The endpoint is fire-and-forget, which is the contract of the webhook it replaces.

**The allowlist is a new entity, `webhook_endpoints`, not configuration.** Templates, destinations
and routes are all in the database, and configuration holds infrastructure and credentials only;
an endpoint belongs with the former. It also means adding one is a form post rather than a restart.
The alternative — a list in `config.yaml` — was rejected on both counts, and because kong's struct
tags express a list of structs badly.

**An endpoint names a `Destination`, not a team and a channel of its own.** The channel is already
configured, already chosen through the Teams picker, and already bounded by the grants that decide
who may deliver where. Naming it twice would let the two spellings disagree, and would put a
second, ungranted path into a channel.

**The secret travels in the path, and each endpoint has its own.** That is what lets a sender which
can only be handed a new URL migrate at all. Because the endpoint is a row anyway, the token is per
endpoint: revoking one sender does not break the others, which a single shared token could not
offer. Only a SHA-256 digest is stored — unsalted, because the token is 32 random bytes generated
here rather than a password somebody chose, so there is no dictionary to stretch against.

**The token is shown once, and the post that generates it answers by rendering rather than
redirecting.** Every other form post in the admin UI answers with a redirect carrying a notice in
the query string. A redirect cannot be used here: it would put a live secret into the browser
history and into this service's own access log.

**An unconfigured slug pair answers `404` and a wrong token `401`.** This tells an unauthenticated
caller which pairs exist. We accept that: the token still gates posting, the pair is not itself a
secret, and somebody wiring up a sender has to be able to tell "I typed the channel wrong" from "I
typed the secret wrong". Hiding the difference would make every misconfiguration look the same.

**All three payload shapes are accepted, and the shape is recognised from the body.** Not from a
content type or a query parameter: a sender being migrated sends what it always sent and is not
going to start labelling it. An envelope's Adaptive Card is forwarded to Graph byte for byte,
because passing it through untouched is the whole point; text is sanitized through the same
allow-list the template path uses; a MessageCard is converted into one Adaptive Card.

**A MessageCard's `potentialAction` survives only as `Action.OpenUrl`.** `HttpPOST`, `ActionCard`
and `InvokeAddInCommand` each need the connector to call the sender back, and nothing here can.
They are dropped rather than rendered as buttons that do nothing. `themeColor` is mapped onto the
five container styles Adaptive Cards has, by hue, because the colour is what a reader of a
connector card reacts to first.

**`graph.Message` carries `Cards []json.RawMessage` rather than one card.** A V2 payload may
legitimately bring several attachments. The alternative, rejecting the second one, trades a `400`
in the middle of somebody's migration for a struct field staying narrower.

**The endpoint is registered as a `ServeMux` wildcard pattern.** Every other route in this server
registers a prefix and trims it by hand. This path has two meaningful segments and a secret, and
naming them keeps `r.Pattern` — and so the route label on every HTTP metric — one string rather
than one per endpoint.

**A webhook endpoint is excluded from the export bundle**, beside recipients. It carries a
credential, and [ADR 0013](0013-configuration-bundle.md) says a bundle carries none. Exporting one
without its token would produce an endpoint no sender could reach; exporting it with the token
would put a live secret in a file whose purpose is being copied around.

## Consequences

A sender migrates by changing one URL. That is the point, and it is the first ingest path this
service offers that does not produce an alert.

It is also a second way into a Teams channel, authenticated by a secret in a URL rather than by a
header or a session. A URL that leaks — into a screenshot, a ticket, a shell history — is a live
credential until somebody rotates it, which is why rotation is one button and why the token is per
endpoint rather than shared. Operators should expect webhook URLs to need the same handling as any
other secret.

Two paths now render a message, and they sanitize by the same call but decide the summary line
differently. A MessageCard's title becomes the message title rather than a block inside the card,
so the Teams activity feed has something to preview.

The schema grows one table, additively, so the previous release runs against it unchanged.

Not decided here: whether a Teams V2 message should ever be updated in place. Nothing identifies a
later post as the same event, so nothing could find the card to update. If a sender ever needs
that, it needs an identifier in the payload, and that is a different decision.
