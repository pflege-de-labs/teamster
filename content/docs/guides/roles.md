---
title: Manage roles and access
weight: 19
---

Give people a role, collect them in groups, let them use the webhooks, share single records with
them, and limit a role to some Teams and channels.

How roles, owners and Cedar policies fit together is explained in
[Ownership and sharing](../../concepts/ownership/). This page is the tasks.

## Give someone a role

Roles come from your identity provider, one to one by name. Assign a provider role called `admin`,
`editor` or `viewer`, and the user holds that role at their next sign-in.

| Role | May |
| --- | --- |
| `admin` | Everything, including whatever later releases add |
| `editor` | Read the configuration and change it |
| `viewer` | Read it, and link or unlink their own chat |

A user carrying several roles holds all of them. A user carrying none of the three gets
`auth.default-role`, which may be `admin`, `editor`, `viewer` or empty for no access. See
[Configure sign-in](../signing-in/#decide-what-a-user-without-a-role-gets).

A role is decided at sign-in. A change at the provider applies the next time that person signs in.

### Define a role of your own

A role with any other name, for example `auditor`, reaches Teamster under its own name and grants
nothing until a Cedar policy names it. The policies are embedded in the binary, in
[`internal/authz/policies.cedar`](https://github.com/pflege-de-labs/teamster/blob/main/internal/authz/policies.cedar),
so a role of your own means adding a policy for `Role::"auditor"` there and building Teamster.
Adding policies is safe. Removing or renaming `admin`, `editor` or `viewer` breaks the admin UI.

## Check who may do what

* **Access overview** (**/admin/access**) appears in the menu for admins and for anyone a grant
  names, directly or through a group, provider group or role. There they
  [share several records at once](#share-several-records-at-once), and **Every grant** lists the
  grants on the records they may share, with each record's name linking to it.
* Admins also see there who may send to the webhooks, who may message people, every grant, **Who
  can?** and the **Policies in force**. **Who can?** takes a subject, and an action and a resource
  from lists that include the webhooks and **People**, and names the policies that allow or refuse
  it. A policy's text unfolds under its name. Older links with `type=` and `id=` still work.
* **My access** in the user menu (**/admin/me**) shows anyone their own roles, groups, webhooks and
  **People you may message**. **Shared with you** lists every record shared with them, with a link,
  the actions, and **Through** whom: themselves, a group, a provider group or a role. Below it, one
  line per role says what that role allows on all records of a kind. The Cedar policies behind it
  are under **Show the policies behind this**.

A refused request is a `403` naming the role and the resource.

## Collect people in groups

Groups at **/admin/groups** collect users, other groups, and the groups your identity provider
names in `auth.groups-claim` (default `groups`). Editors create and change groups, and everyone can
see them. Groups nest, but a group cannot contain itself.

Permissions on records and webhook levels can be granted to a group as well as to a user. Every
membership change is recorded in the [audit trail](../audit-trail/) in the same transaction.

```bash
# create a group, then add a provider group to it
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/groups -d '{"name": "payments"}'
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/groups/<group id>/members \
  -d '{"type": "idp_group", "id": "/payments-oncall"}'
```

Member `type` is `user`, `group` or `idp_group`. Groups are not part of a configuration bundle.

## Let someone use the webhooks

Editors and admins may use both webhooks. Anyone else needs a webhook level, set on
**/admin/access** per user, group, provider group or role:

| Level | API value | Webhooks |
| --- | --- | --- |
| none | `none` | None |
| Alertmanager | `alertmanager` | `/webhook/alertmanager` |
| Universal | `universal` | `/webhook/universal` |
| both | `all` | Both |
| admin | `admin` | Both, and managing everyone's tokens |

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/access/webhooks \
  -d '{"principal_type": "group", "principal_id": "<group id>", "level": "alertmanager"}'
```

`principal_type` is `user`, `group`, `idp_group` or `role`. With a level, they can issue their own
tokens; see [Authenticate a webhook sender](../webhook-tokens/). Taking the level away revokes those
tokens at their next use.

## Let someone message people

A message that names people needs a token that may name them; see
[Let a token name people](../webhook-tokens/#let-a-token-name-people). Everyone signed in may name
themselves. Naming anyone else takes the level **anyone**, set on **/admin/access** under
**Who may message people** per user, group, provider group or role. Admins hold it already. Grant
it to the people who send notices to colleagues, such as office admins. The level
**anyone, and broadcast to everyone** adds sending one message to everyone the bot can reach; see
[Send a message to everyone](../broadcasts/).

| Level | API value | They and their tokens may name |
| --- | --- | --- |
| themselves only | `none` | Only themselves |
| anyone | `anyone` | Anyone in the tenant |
| anyone, and broadcast to everyone | `everyone` | Anyone, and [everyone at once](../broadcasts/) |

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/access/messages \
  -d '{"principal_type": "idp_group", "principal_id": "<provider group>", "level": "anyone"}'
```

`principal_type` is `user`, `group`, `idp_group` or `role`. `none` takes the level back, and their
**anyone** and **anyone, and broadcast** tokens name only their creator from the next request. In
Cedar, **anyone** is the action `message` on `People::"*"` and **anyone, and broadcast to
everyone** is `broadcast`. Each includes
the levels below it, down to `messageSelf`, the action everyone holds. See
[ADR 0082](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0082-naming-people-takes-permission.md)
and
[ADR 0083](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0083-broadcasts-run-in-the-background.md).

## Share a record

Whoever creates a template, destination, route, webhook endpoint or group owns it. To share one,
edit it and use **Who may do what**:

1. Under **Give to**, choose a user, a group, a provider group or a role from the list. Someone who
   has not signed in yet is not listed: open **Someone not listed**, pick the kind and enter their
   subject, provider group or role name instead.
2. Tick what they **May** do, and press **Save**.

| Action | In the form | Lets them |
| --- | --- | --- |
| `read` | read | See the record |
| `update` | change | Change it |
| `delete` | delete | Delete it |
| `attach` | use in routes and webhooks | Point routes and webhooks at it |
| `share` | share | Give others what they hold themselves |
| `own` | own (everything, and pass ownership on) | Everything, including passing ownership on |

Each principal who already holds something is a row with their actions ticked. Change the boxes and
press **Save** to replace what they hold; with no box ticked, it is all taken away. **Revoke**
removes the row.

Nobody can give more than they hold. Someone with no role but a shared record sees **/admin** with
just what was shared.

`create` on a whole collection is granted through the API:

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/sharing \
  -d '{"principal_type": "group", "principal_id": "<group id>",
       "resource_type": "Route", "resource_id": "*", "actions": ["create"]}'
```

`GET /api/sharing?type=Template&id=<id>` lists a record's permissions, and
`DELETE /api/sharing/<id>` revokes one.

## Share several records at once

{{% steps %}}

### Pick a kind of resource

Open **Access overview** (**/admin/access**), choose a **Kind of resource** (Templates,
Destinations, Routes, Webhook endpoints or Groups) and press **Show**. Only records you may `share`
are listed. If there are none, the page says that there is nothing you may share yet.

### Tick the records

Tick them under **Records**. **Who has access** next to a record opens its **Who may do what** panel
below the form, where you change or revoke single grants; **Edit** opens the record.

### Choose who and what

Choose the principal under **Give to**, or under **Someone not listed**, tick what they **May** do
and press **Grant on selected**.

{{% /steps %}}

Saving replaces what that principal holds on each ticked record, all or nothing: if you may not
grant it on one record, nothing is saved and the error names that record's ID. A group must exist. A
user who has not signed in yet is accepted, and the notice says the access applies once they do.
`own` is not offered here; pass ownership on from a single record.

## Disable a user

**/admin/users** (admins) lists everyone who has signed in, with the roles and groups of their last
sign-in. **Disable** ends that user's sessions at once, refuses their next sign-in and stops their
webhook tokens. **Enable** reverses it. Nobody can disable themselves, and the local login cannot be
disabled.

```bash
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/users/disabled \
  -d '{"subject": "<subject>"}'
```

`DELETE` on the same path enables the user again. `GET /api/users?q=<text>` searches the list.

## Limit a role to some Teams and channels

{{% steps %}}

### Open Permissions

Open **Permissions** (**/admin/permissions**) as an admin and pick a role.

### Tick what it may reach

Tick Teams and channels in the tree. Ticking a Team grants all its channels, including ones added
later. Ticking channels individually grants only those.

### Save

Saving replaces what the role had, in one transaction.

{{% /steps %}}

{{< callout type="info" >}}
A role with no grant reaches everything. Narrowing starts with its first grant. Admins are never
limited.
{{< /callout >}}

Grants apply to what a session may **see** as well as where it may deliver. The Team and channel
pickers offer only what is granted, a destination outside the grants is absent from the lists and
the API, and a write or a route pointing outside them is refused with `403`.

Scripts use `PUT /api/grants/role`, which replaces a role's grants like the page does:

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/grants/role \
  -d '{"role": "editor", "scopes": [{"team_id": "<team id>"},
       {"team_id": "<team id>", "channel_id": "<channel id>"}]}'
```

An empty `channel_id` means the whole Team. `/api/grants` adds and lists single grants.
