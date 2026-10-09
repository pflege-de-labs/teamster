---
title: Roles and ownership
weight: 3
---

Teamster decides who may do what in two layers: a **role** that comes from your identity provider
and covers the whole configuration, and **permissions on single records** that owners hand out.
Both are evaluated as [Cedar](https://www.cedarpolicy.com/) policies built into the binary. To
assign roles, share records and manage groups, see [Manage roles and access](../../guides/roles/).

## Roles

Three roles are built in, each including the one below it:

| Role | May |
| --- | --- |
| `admin` | everything, including whatever later releases add |
| `editor` | read the configuration and change it |
| `viewer` | read it, and link or unlink their own chat |

**Roles map one to one, by name.** A provider role called `editor` is the Teamster role `editor`.
A role called anything else, such as `auditor`, arrives under its own name. It grants nothing
until a policy mentions it, which is why passing every role through is safe.

**A role is decided at sign-in** and travels with the session. A change at the provider applies
the next time that person signs in. Someone whose claim names none of the three gets
`auth.default-role`; leave it empty and they sign in with no access.

**The local login is always an admin.** It is the way back in when the identity provider is down
or its claim is wrong.

### Narrowing a role to Teams and channels

An admin can limit where a role may deliver, by granting it specific Teams or channels. A role
with no grant reaches every Team and channel. As soon as one grant names a role, it reaches only
what its grants name. Grants also limit what that role sees: the pickers and lists show only
granted Teams and channels. Admins are never limited.

## Owning and sharing records

Whoever creates a template, destination, route, webhook endpoint or group **owns** it. The owner,
or anyone holding `share` on it, can give a user, a group, a provider group or a role actions on
that one record:

| Action | Lets them |
| --- | --- |
| `read` | see the record |
| `update` | change it |
| `delete` | delete it |
| `attach` | point routes and webhooks at it, such as routing into a destination or rendering with a template |
| `share` | give others what they hold themselves |
| `own` | everything, including passing ownership on |

Nobody can give more than they hold. Granting or revoking `own` makes you an owner as well.

To give one principal the same actions on several records of a kind at once, use **Access
overview** (`/admin/access`); see
[Share several records at once](../../guides/roles/#share-several-records-at-once). **My access**
(`/admin/me`) lists what has been shared with you, and through whom.

Sharing adds to roles; it does not replace them. Editors keep editing everything. Someone with no
role but a shared record sees `/admin` with just what was shared. Pages built from the whole
configuration stay closed to them: the routing graph, previews and the export.

## Groups

A group collects users, other groups, and the groups your identity provider sends in
`auth.groups-claim`. You grant permissions on a record to a group the same way as to a user, so
access follows membership. Groups nest, but a group can never contain itself.

Groups are local to an installation: a configuration bundle does not carry them.

## Webhook access

Sending to a webhook is a permission of its own. Editors and admins may use both webhooks. Anyone
else needs a webhook level granted to them, their group or their role: none, Alertmanager,
Universal, both, or admin, which also manages everyone's tokens.

A webhook token sends only where its creator may send, checked again on every request. Disabling
the creator or taking their level away revokes their tokens at once. See
[Issue webhook tokens](../../guides/webhook-tokens/).

Naming people in a message is a permission too. Everyone may name themselves; naming anyone else
takes a grant, which admins hold. A token names people only as far as its own message level and its
creator's permission both allow. See
[Let someone message people](../../guides/roles/#let-someone-message-people).

## Every change is recorded

Changes to permissions, group memberships and disabled users are written to the audit trail. A
group membership change is recorded in the same transaction: if the record cannot be written, the
change does not happen. See
[Audit trail](../../guides/audit-trail/).
