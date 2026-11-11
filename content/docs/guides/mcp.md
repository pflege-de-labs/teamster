---
title: Connect an AI assistant over MCP
weight: 23
---

Let an AI assistant such as Claude Code read, and optionally change, your templates, destinations
and routes through Teamster's MCP endpoint, as the person who gave it a token.

MCP is off until an operator turns it on. Nothing below works before that.

## Enable MCP

```yaml
mcp:
  enabled: true     # serve /mcp and let users mint MCP tokens at /admin/tokens
  read-only: false  # refuse every tool that writes, whoever the user is
```

Or `TEAMSTER_MCP_ENABLED=true` and `TEAMSTER_MCP_READ_ONLY=true`. Both default to `false`. See
[Configuration](../../reference/configuration/#mcp).

`read-only` is a deployment-wide switch. With it on, the endpoint offers no write tool and refuses
a write even if a client calls one, whatever the token or its user allows.

## Mint a token

Every signed-in user, a viewer included, can mint an MCP token. It is separate from the
[webhook tokens](../webhook-tokens/) on the same page.

{{% steps %}}

### Create it

Open **Access → Access tokens** (**/admin/tokens**) and find **MCP access tokens**. Name the token
after the client, for example `claude-code-laptop`; names are unique per user. Choose the **Access**:
**read only**, or **read and write**. Select **Create token**.

### Copy it

The token, starting with `tmcp_`, is shown once. Teamster stores only a digest, so it cannot show it
again. Put it in the client's secret store.

{{% /steps %}}

You see and revoke the tokens you minted, with **Revoke**. Webhook admins and admins see and revoke
everyone's. A revoked token is refused from its next request.

Minting and revoking are recorded in the [audit trail](../audit-trail/).

## What a token can do

A token acts as the person who minted it, with their permissions as they are now, and never more.

* It stops working when its creator is disabled.
* **Read only** cannot write, whatever the creator may do. **Read and write** can change what the
  creator may change, and only while `mcp.read-only` is off.
* It opens `/mcp` only. It does not open the webhooks or `/api`.
* Every call goes through the admin API as the creator, so roles, grants and ownership apply as in
  the UI. See [Manage roles and access](../roles/).

The tools:

| Access | Tools |
| --- | --- |
| Read | `list_templates`, `get_template`, `list_destinations`, `get_destination`, `list_routes`, `get_route`, `preview_template`, `match_route`, `list_teams`, `list_channels`, `list_sampled_labels` |
| Write | `create_template`, `update_template`, `delete_template`, `create_destination`, `update_destination`, `delete_destination`, `create_route`, `update_route`, `delete_route` |

Write tools go only to a **read and write** token while `mcp.read-only` is off.
`list_sampled_labels` needs [event samples](../../reference/configuration/#event-samples)
on, and a user who may edit templates or routes.

## Connect an assistant

Point the client at `/mcp` on your Teamster URL, with the token as a bearer credential.

{{< tabs >}}

{{< tab name="Claude Code" >}}

```bash
claude mcp add --transport http teamster https://teamster.example.com/mcp \
  --header "Authorization: Bearer <token>"
```

{{< /tab >}}

{{< tab name="Any MCP client" >}}

Use the streamable HTTP transport with the URL `https://teamster.example.com/mcp` and the header
`Authorization: Bearer <token>`. The endpoint is stateless: it keeps no session between requests.

{{< /tab >}}

{{< /tabs >}}

## Install the Claude Code plugin

The Teamster repository is a Claude Code plugin marketplace. The plugin `teamster` connects Claude
Code to the MCP endpoint and adds the skill `teamster-routing`.

```text
/plugin marketplace add pflege-de-labs/teamster
/plugin install teamster@teamster
```

Claude Code asks for two values:

* **Teamster URL**: the base URL without `/mcp`, for example `https://teamster.example.com`.
* **MCP token**: the `tmcp_` token you minted. The plugin keeps it in Claude Code's secure
  credential storage.

The skill guides the assistant through routing an alert: read the sampled labels, dry-run with
`match_route`, pick a destination, preview the template, write the route and confirm with
`match_route` again. It asks you before it deletes anything.

## Keep it safe

Label values, annotations, template text and the names others chose reach the assistant, and anyone
who can send an event or edit a record can write text there. Treat it as untrusted input.

* **Prefer a read only token.** Mint a **read and write** token only for an assistant that must
  change things.
* **Set `mcp.read-only`** on a deployment where no assistant should ever write.
* **Give each client its own token**, so you can revoke one without breaking the others.
* **Audit it.** Changes made through MCP appear in the audit trail with `via` set to `mcp`, under
  the name of the token's creator.

See
[ADR 0106](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0106-mcp-tokens-answer-to-their-creator.md)
and
[ADR 0107](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0107-mcp-endpoint-over-the-admin-api.md)
for the design.
