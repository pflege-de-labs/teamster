# 0060. Directory users are kept apart from linked recipients

* Status: Accepted
* Date: 2026-09-30

## Context

[ADR 0059](0059-install-the-teams-app-for-every-member.md) installs the Teams app for every member
of the tenant, so Teamster has to remember thousands of people: who they are, the chat the bot has
with them, and how far installing the app got.

`recipients` already holds people with a chat
([ADR 0026](0026-alerts-in-a-persons-chat.md)). But a recipient is something else:

* It is keyed by an **admin-UI subject**, with a unique index on it. It records that someone signed
  in and linked their own chat. A directory user has no subject, and most never sign in.
* Every recipient is shown to editors: in the route target select, on `/admin/recipients`, and in
  `/status`. Thousands of employees there would bury the handful of people who linked a chat.
* The Cedar action `deliverToRecipient` is keyed by the subject
  ([ADR 0047](0047-a-route-targets-a-channel-or-yourself.md)).
* A recipient is deleted when the person unlinks. A directory user is never unlinked while global
  install is on.

## Decision

We will keep directory users in their own table, `directory_users`, keyed by Entra object id.

* It holds what Graph last said (UPN, mail, names, whether the account is an enabled member), with
  lower-cased copies of the UPN and mail for lookups that ignore case.
* It holds the chat (conversation id and service URL), the install state and the retry schedule.
* It keeps its own `blocked_at` and `blocked_reason`, the same informational flag recipients have.
* A person who is both keeps one row in each. When an admin-UI user is bound to their own chat, a
  later change will match on object id and copy the conversation into their recipient. The two
  tables stay separate.

Install runs get a table of their own, `directory_runs`. A row records one run's progress, and a
partial unique index admits one active run, so the table is also the lease across replicas. A
separate lock table, or an advisory lock, was not chosen: SQLite has no advisory locks, and the
progress counters would still need somewhere to live.

Alternatives considered:

* **Recipients with a synthetic subject.** No schema change, but every screen that lists recipients
  would need a filter, and anything that forgot one would leak the whole directory into it.
* **Recipients with a nullable subject.** Loosens a unique key other code relies on, and would need
  the table rebuilt in SQLite.

## Consequences

* Two tables can describe the same person's chat. They agree because both are written from the same
  install events. A person holding two admin-UI subjects already had two recipient rows.
* Addressed delivery tracks its claims under a key that says which table the person is in. A later
  ADR records how.
* `directory_users` is never exported ([ADR 0013](0013-configuration-transfer.md)): it is runtime
  state and personal data, like recipients.
* People who left are marked `departed` after a complete listing that did not return them, and
  purged 30 days later.
* Additive: two new tables, nothing else changed.
