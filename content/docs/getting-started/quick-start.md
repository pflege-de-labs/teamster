---
title: Quick start
weight: 1
---

In this tutorial you run Teamster on your machine, point a route at a Teams channel and send it an
alert. Then you resolve the alert and watch the card change.

## Before you start

You need:

* An Entra app registration for Microsoft Graph, with its tenant id, client id and client secret.
  Teamster uses it to list your Teams and channels. See
  [Grant the Microsoft Graph permissions](../../guides/graph-permissions/).
* A second Entra registration for the Teams bot, with its client id and client secret. The bot
  posts every card. See [Set up the Teams bot](../../guides/teams-bot/).
* The Teamster Teams app installed in the team you want to post to.
* `curl`, and either Docker or a Linux or macOS machine to run the binary on.

{{< callout type="info" >}}
Teamster starts without the bot, but then no card reaches a channel: every delivery fails with a
`502` that says the bot is not configured.
{{< /callout >}}

{{% steps %}}

### Write a configuration file

Create `config.yaml` in an empty directory:

```yaml
admin:
  username: admin
  password: <admin-password>

graph:
  tenant-id: <tenant-id>
  client-id: <graph-client-id>

bot:
  tenant-id: <tenant-id>
  client-id: <bot-client-id>
```

Keep the two client secrets out of the file and export them instead. Teamster reads an environment
variable only when no config file sets that key.

```bash
export TEAMSTER_GRAPH_CLIENT_SECRET='<graph-client-secret>'
export TEAMSTER_BOT_CLIENT_SECRET='<bot-client-secret>'
```

Everything else keeps its default: the server listens on `:8080` and stores its state in a SQLite
file. [Configure Teamster](../../guides/configuration/) explains where else the configuration can
come from.

### Start the server

{{< tabs >}}
{{< tab name="Binary" >}}

Download the release binary for your platform (`linux-amd64`, `linux-arm64`, `darwin-amd64` or
`darwin-arm64`) and run it with the configuration file:

```bash
curl -fLo teamster \
  https://github.com/pflege-de-labs/teamster/releases/latest/download/teamster-linux-amd64
chmod +x teamster
./teamster -c config.yaml
```

The database is written to `teamster.db` in the current directory.

{{< /tab >}}
{{< tab name="Container" >}}

Mount the configuration at the system-wide location Teamster searches, and keep the database on a
volume:

```bash
docker run --rm -p 8080:8080 \
  -e TEAMSTER_GRAPH_CLIENT_SECRET -e TEAMSTER_BOT_CLIENT_SECRET \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  ghcr.io/pflege-de-labs/teamster:latest
```

{{< /tab >}}
{{< /tabs >}}

`serve` is the default command, so it runs when you give none. The server stops on `Ctrl+C`.

### Sign in

Open <http://localhost:8080/admin> and sign in with the admin username and password from
`config.yaml`.

### Add a destination

On the **Destinations** tab, give the destination a name, pick the **Team** and the **Channel**,
and save it.

The first destination you create becomes the **global default destination**: whatever no route
claims goes there.

### Add a route

On the **Routes** tab, create a route:

| Field | Value |
| --- | --- |
| Name | `Worker alerts` |
| Label selector (JSON) | `{"service": "worker"}` |
| Delivers to | the destination you just created |
| Template | `Universal webhook (default)` |

Leave **Refines** empty, so this is a root route.

### Issue a token for the sender

Open <http://localhost:8080/admin/tokens>. Create a token named `quick-start` that may send to the
universal webhook, and copy it. It is shown only once.

```bash
export TOKEN='<token>'
```

### Send an alert

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8080/webhook/universal --data '{
    "key": "quick-start-1",
    "state": "open",
    "labels": {"alertname": "HighMemory", "severity": "warning", "service": "worker"},
    "attributes": {
      "summary": "Memory usage is above 80%",
      "description": "worker service memory is high"
    }
  }'
```

Teamster answers `{"status":"ok"}`, and a card for `HighMemory` appears in the channel. The label
`service=worker` matched your route.

### Resolve the alert

Send the same `key` again with `state` set to `closed`:

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8080/webhook/universal --data '{
    "key": "quick-start-1",
    "state": "closed",
    "labels": {"alertname": "HighMemory", "severity": "warning", "service": "worker"},
    "attributes": {"summary": "Memory usage is back to normal"}
  }'
```

The existing card changes to the resolved state. Teamster does not post a second card, because it
remembers which card belongs to the key.

{{% /steps %}}

## Next steps

* Connect Prometheus: [Send alerts from Alertmanager](../../guides/alertmanager/).
* Design your own cards: [Write templates](../../guides/templates/).
* Understand which route an alert takes: [Routing]({{< ref "/docs/concepts/routing" >}}).
* Run it for real: [Container]({{< ref "/docs/getting-started/container" >}}) or
  [Kubernetes]({{< ref "/docs/getting-started/kubernetes" >}}).
