---
title: Send alerts from Alertmanager
weight: 10
---

Point a Prometheus Alertmanager receiver at Teamster, and each group of alerts arrives in Teams as
one card that is updated while its alerts fire and closed when they resolve.

You need a running Teamster and a token that may send to the Alertmanager webhook. See
[Authenticate a webhook sender](../webhook-tokens/) to issue one.

## Configure the receiver

{{% steps %}}

### Issue a token

Sign in to the admin UI, open **/admin/tokens** and name the token after the sender (for example
`alertmanager-prod`). Under **May send to** both webhooks start ticked; untick
`/webhook/universal`, so the token reaches `/webhook/alertmanager` only. Copy the token at once:
it is shown only this once.

Store it where Alertmanager can read it, for example in a Kubernetes Secret:

```bash
kubectl -n monitoring create secret generic teamster-webhook --from-literal=token=<token>
```

### Add the receiver

The endpoint is `POST /webhook/alertmanager`. The token goes in `Authorization: Bearer`. Set
`send_resolved`, or the card is never closed.

{{< tabs >}}
{{< tab name="alertmanager.yml" >}}

```yaml
receivers:
  - name: teamster
    webhook_configs:
      - url: http://teamster.monitoring.svc:8080/webhook/alertmanager
        send_resolved: true
        http_config:
          authorization:
            type: Bearer
            credentials_file: /etc/alertmanager/secrets/teamster/token
```

Mount the Secret at `/etc/alertmanager/secrets/teamster/`.

{{< /tab >}}
{{< tab name="AlertmanagerConfig" >}}

For the Prometheus Operator:

```yaml
receivers:
  - name: teamster
    webhookConfigs:
      - url: http://teamster.monitoring.svc:8080/webhook/alertmanager
        sendResolved: true
        httpConfig:
          authorization:
            type: Bearer
            credentials:
              name: teamster-webhook
              key: token
```

{{< /tab >}}
{{< /tabs >}}

Do not use `basic_auth`. Teamster accepts only a bearer token on the webhooks.

### Route alerts to the receiver

Add a route in Alertmanager that sends the alerts you want to the `teamster` receiver. Its
`group_by` decides which alerts share a card. Which Teams channel a group reaches is decided in
Teamster, by its routes. See [How routing works](../../concepts/routing/).

### Test it

Post a sample alert by hand:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  --data @alertmanager-firing.json http://localhost:8080/webhook/alertmanager
```

Take
[`alertmanager-firing.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/alertmanager-firing.json)
and
[`alertmanager-resolved.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/alertmanager-resolved.json)
from the repository, or
[`alertmanager-group.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/alertmanager-group.json)
for a group of two alerts. A `200 {"status":"ok"}` means the notification was delivered.

{{% /steps %}}

## What Teamster does with a notification

* A notification is one event, however many alerts Alertmanager grouped into it. Its `groupKey`
  is the key, so every notification for the group edits the same card.
* The notification's `status` is the state. It is `firing` while any alert in the group fires,
  which opens the card, and `resolved` once all have, which closes it. Any other status is
  delivered once and not tracked. See
  [Alert lifecycle](../../concepts/alert-lifecycle/).
* Routes select on `commonLabels`, the labels all alerts in the group share. For a group of one
  alert, those are the alert's labels.
* Every event carries the label `teamster_source: alertmanager`. A route can select on it.
* Templates find the group's alerts in `.Event.Alertmanager.Alerts`. The flat `Annotations`,
  `StartsAt`, `EndsAt` and `GeneratorURL` are set only for a group of one alert. See
  [List the alerts of a group](../templates/#list-the-alerts-of-a-group) and
  [Template data](../../reference/template-data/#alertmanager-extension).
* To send an alert to people rather than a channel, set the label `teamster_recipient`. The
  addresses of all alerts in the group are joined. See
  [Send messages to individual people](../direct-messages/).

## After upgrading from a release with one card per alert

Releases up to 0.11.0 posted one card per alert. After the upgrade:

* A group that is already firing gets a new group card beside its old per-alert cards. Each old
  card is closed when Alertmanager reports its alert as `resolved`.
* The Alertmanager default template seeded by the earlier release does not list a group's alerts.
  Apply the preset again, as described in
  [List the alerts of a group](../templates/#list-the-alerts-of-a-group).

## If Alertmanager reports an error

| Alertmanager logs | Means |
| --- | --- |
| `unexpected status code 401` | Teamster did not accept the token. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `unexpected status code 403` | A mesh or gateway refused it, the token's scope does not allow this webhook, or an alert sets `teamster_recipient` and the token may not name people. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `unexpected status code 422` | The notification addressed people and none of them could be reached. Alertmanager does not retry. |
| `unexpected status code 502` | Teams, Graph or the database failed. Alertmanager retries. |
| `unexpected status code 503` | The token could not be checked because the database did not answer. Alertmanager retries. |
