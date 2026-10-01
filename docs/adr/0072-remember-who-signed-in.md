# 0072. Remember who signed in, and let an admin disable them

* Status: Accepted
* Date: 2026-10-01

## Context

Teamster knows a person only through a session, which lasts `auth.session-ttl` and is swept once it
expires. Groups, record ownership and per-user permissions are planned (milestone 25 in the
[roadmap](../roadmap.md)), and each of them names a user. Granting something to a user needs a
list to pick them from, and a name to show beside their subject. A token minted by a user also needs
its creator's current standing when the token is used.

There is also no way to lock someone out short of removing them at the identity provider. Teamster
holds its own state about people, and will hold more once ownership exists, so an admin needs a
switch of their own.

## Decision

We will keep a `users` row per subject, written at every sign-in, and let an admin disable a user.

* `startSession` upserts the row, which holds:
  * `subject` and `source` (`oidc` or `local`), `name` and `email`
  * the roles and the IdP groups from this sign-in
  * `first_seen` and `last_seen`

  The write is best effort: a failure is logged and the sign-in goes ahead. The next sign-in
  writes the row again.
* Disabling sets `disabled_at` and `disabled_by`, and deletes the user's sessions and their
  broker tokens in the same transaction. Signing in again is refused while the row is disabled. A
  later sign-in refreshes the other fields but keeps the disabled state.
* Because disabling ends every session, a request needs no per-request check of the registry.
* An admin cannot disable the local login, because it is the way back in. They cannot disable
  themselves either.
* `/admin/users`, `GET /api/users` and `POST`/`DELETE /api/users/disabled` are the `administer`
  action on `User`, so they are admin-only. Disabling and enabling are audited as `user.disable`
  and `user.enable`. A sign-in is not audited, because it is not a configuration change.
* Rows are not backfilled from live sessions. A session lasts at most a day, so every active user
  appears within one session lifetime.

Alternatives considered:

* **Checking the registry on every request.** It costs a lookup per request and makes the session
  table's own lifetime meaningless. Ending the sessions at the moment of disabling has the same
  effect.
* **Reading users from the identity provider.** It needs provider-specific admin APIs and
  credentials. Teamster only needs the people who have actually used it.

## Consequences

* Permissions can name a user by subject and show who that is.
* A role or group change at the provider reaches the registry at the user's next sign-in, not
  earlier.
* `users` and the index `sessions_subject` are new: SQLite `0024`, Postgres `0021`. The previous
  release ignores the table and leaves rows unwritten while it runs, which the next sign-in under
  this release repairs.
* A disabled user's API access through basic auth is unaffected: basic auth is the local
  credentials, which cannot be disabled.
