# 0074. Local groups, with provider groups as members

* Status: Accepted
* Date: 2026-10-01

## Context

Per-record permissions (milestone 25 in the [roadmap](../roadmap.md)) need someone to grant them
to. Granting to individual users does not scale past a handful of people. The identity provider
already sends groups (`auth.groups-claim`), but [ADR 0043](0043-session-keeps-sign-in-identity.md)
keeps them for display only. Provider groups are also often too coarse, or named for the
organisation chart rather than for who looks after which alerts.

Membership decides access, so changing it is a permission change. A membership change that is
lost from the audit trail is a hole in the record of who could do what. A group that ends up
containing itself would leave Cedar's `in` with nothing sensible to answer.

## Decision

We will add local groups.

* **Members.** A local group's members are users (by subject), other local groups, and provider
  groups (by the name the claim carries).
* **Cedar entities.** Groups become entities in the policy snapshot
  ([ADR 0073](0073-authorize-from-a-versioned-policy-snapshot.md)):
  * `Group::"<id>"` and `IdpGroup::"<name>"` have as parents the local groups that list them.
  * A request's `User::"<subject>"` has as parents its roles, the local groups that list its
    subject, and an `IdpGroup` for every group in its session's `Identity.Groups`.

  So `principal in Group::"oncall"` holds for a direct member, a member of a nested group, and
  anyone whose provider group is listed, however deep.
* **Tables.** `user_groups` and `user_group_members`, named so because `GROUPS` is a keyword in
  both dialects.
* **Every write in one transaction.** Each group write runs in a transaction together with:
  * the authz generation bump;
  * for adding a member, a cycle check inside a serializable transaction. The check refuses a
    membership when the new member already contains the group.

  Deleting a group removes its members and its own memberships.
* **Audit fails closed.** When the database trail is on, a group change writes its audit row inside
  that same transaction. No record means no change. These are the first changes audited this way;
  ordinary configuration stays fail-open ([ADR 0070](0070-audit-configuration-changes-at-the-store.md)).
* **Who may manage.** `/admin/groups` and `/api/groups` are the `Group` resource. Viewers see the
  groups, and editors and admins change them. Owners come with per-record permissions.
* **Not exported.** Groups are left out of the configuration bundle. They name user subjects and
  provider groups that mean nothing in another tenant.

In this step no policy names a group, so groups decide nothing yet. Per-record permissions grant to
them in the next step. This supersedes the "groups decide nothing" part of ADR 0043 for provider
groups as well.

Alternatives considered:

* **Provider groups only.** No local state, but every grouping would need an identity provider
  change, which the people looking after alerts rarely control.
* **Expanding groups to user lists when the snapshot is built.** This works for local members.
  Provider groups are only known per session, so they would have to be expanded per request anyway.
  Entity parents let Cedar do both.

## Consequences

* Provider group membership changes reach Teamster at the user's next sign-in, along with their
  roles.
* Renaming a provider group at the provider orphans local memberships naming the old name.
  They match nobody and are listed under the old name until removed.
* `user_groups` and `user_group_members` are new tables (SQLite `0026`, Postgres `0023`). The
  previous release ignores them.
