---
title: Send alerts from Alertmanager
weight: 10
---

Point a Prometheus Alertmanager receiver at Teamster, and its alerts arrive in Teams as cards
that are updated while they fire and closed when they resolve.

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

Add a route in Alertmanager that sends the alerts you want to the `teamster` receiver. Which Teams
channel each alert reaches is decided in Teamster, by its routes. See
[How routing works](../../concepts/routing/).

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
from the repository. A `200 {"status":"ok"}` means the alert was delivered.

{{% /steps %}}

## What Teamster does with an alert

* Each alert in the group becomes one event. Its `fingerprint` is the key, `firing` becomes the
  state `open` and `resolved` becomes `closed`. Any other status is delivered once and not tracked.
* A repeated `firing` alert edits the card it already posted. A `resolved` alert closes it. See
  [Alert lifecycle](../../concepts/alert-lifecycle/).
* Every event carries the label `teamster_source: alertmanager`. A route can select on it.
* Annotations, `generatorURL` and the group's fields are available to templates under
  `.Event.Alertmanager`. See [Template data](../../reference/template-data/).
* To send an alert to people rather than a channel, set the label `teamster_recipient`. See
  [Send messages to individual people](../direct-messages/).

## If Alertmanager reports an error

| Alertmanager logs | Means |
| --- | --- |
| `unexpected status code 401` | Teamster did not accept the token. See [When a sender is refused](../webhook-tokens/#when-a-sender-is-refused). |
| `unexpected status code 403` | A mesh or gateway refused it, or the token's scope does not allow this webhook. |
| `unexpected status code 422` | The alert addressed people and none of them could be reached. Alertmanager does not retry. |
| `unexpected status code 502` | Teams, Graph or the database failed. Alertmanager retries. |
| `unexpected status code 503` | The token could not be checked because the database did not answer. Alertmanager retries. |
