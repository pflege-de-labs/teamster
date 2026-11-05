# 0093. Hand out webhook URLs from a configured base URL

* Status: Accepted
* Date: 2026-11-05

## Context

The admin UI hands out two URLs a sender is pointed at: a Teams V2 webhook URL, token included,
and the Alertmanager snippet on `/admin/tokens`. Both were built from the request — its scheme and
`Host` — on the reasoning that the host the admin reads the page on is the host the sender reaches.

That stops holding once the admin UI and the webhooks are published separately, as the chart's
`httpRoute.internal` and `httpRoute.external` allow: the admin reads the page on a private
hostname, and the URL they copy names that hostname, which senders on the internet cannot reach.

`server.external-url` is the admin UI's address, which in that deployment is the private one, so
it cannot serve. `X-Forwarded-Host` names the host the request came in on, which is still the
admin's.

## Decision

We will add `server.webhook-url`, an absolute `http` or `https` URL, validated like
`server.external-url`. When set, every URL handed out to a sender starts with it; when empty, the
URL is built from the request as before.

Alternatives:

* **Reuse `server.external-url`.** Wrong in exactly the deployment that has the problem.
* **Derive it in the chart from `httpRoute.external.hostnames`.** Guesses the scheme, and the
  first of several hostnames, and does nothing for a deployment outside the chart.

## Consequences

* Nothing changes for a deployment that serves everything under one hostname.
* A split deployment sets `server.webhook-url` to its public address. URLs already handed out are
  not touched; a Teams V2 URL handed out with the private host has to be corrected in its sender
  by hand, the token stays the same.
