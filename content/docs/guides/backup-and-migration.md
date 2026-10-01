---
title: Back up and move the configuration
weight: 5
---

Teamster's configuration moves as one JSON bundle. Use it to back up an installation, keep the
configuration in a repository, or carry it to another installation or database.

## Know what the bundle carries

| Carried | Not carried |
| --- | --- |
| templates, including the default templates | credentials, webhook tokens included |
| destinations, with Team and channel names beside the ids | groups and record permissions |
| routes | linked people (recipients) |
| permission grants | sessions, login flows and open alerts |

Because the bundle holds no credentials, you can commit it beside the rest of a deployment's
configuration. Where the bundle lands:

* Issue new webhook tokens; see [Authenticate webhook senders](../webhook-tokens/).
* Records the bundle creates are owned by the admins until someone shares them; see
  [Manage roles and sharing](../roles/).
* People link their chats again; see [Set up the Teams bot](../teams-bot/#link-a-persons-chat).

{{< callout type="warning" >}}
An import refuses a bundle with a route that delivers to a person, rather than creating a route
that never delivers. Clear the person from such routes before you export.
{{< /callout >}}

## Export the configuration

From the command line:

```bash
teamster export -o teamster.json
```

`teamster export` reads the database directly, so it works whether or not the server runs. Without
`-o` it writes to stdout. It never migrates the schema; a database with pending migrations is
refused. The command line export carries Team and channel ids only, without their names.

Over HTTP, as an admin:

```bash
curl -u admin:<password> http://localhost:8080/api/config/export > teamster.json
```

The HTTP export also adds the Team and channel names it can resolve through Graph.

## Import a bundle

Preview first, then apply:

```bash
teamster import teamster.json --dry-run
teamster import teamster.json
```

`-` as the file name reads the bundle from stdin. Pick a mode with `--mode`:

| Mode | What happens |
| --- | --- |
| `merge` (default) | Create or update what the bundle carries; leave everything else alone. |
| `replace` | Make the installation match the bundle, deleting what it does not mention. |

```bash
teamster import teamster.json --mode replace --dry-run
```

Both modes validate the whole bundle first and apply it in one transaction, so a bundle that does
not apply changes nothing. `--dry-run` runs the real import and rolls it back, so the preview is
exact.

Over HTTP, as an admin:

```bash
curl -u admin:<password> -X POST --data-binary @teamster.json \
  'http://localhost:8080/api/config/import?mode=replace&dry-run=true'
```

## Move to another tenant

Ids are preserved, so re-importing a bundle where it came from changes nothing. Team and channel
ids belong to one tenant, though: a bundle carried to another tenant imports cleanly and then
delivers nowhere.

The HTTP import names the destinations this tenant cannot resolve. Fix those destinations' Team and
channel ids after the import
([ADR 0013](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0013-configuration-transfer.md)).

To move between databases rather than tenants, see
[Move from SQLite to Postgres](../storage/#move-from-sqlite-to-postgres).
