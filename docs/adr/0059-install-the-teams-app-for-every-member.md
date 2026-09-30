# 0059. Install the Teams app for every member of the tenant

* Status: Accepted
* Date: 2026-09-30

## Context

IT wants to message individual employees, for example to say that a password expires in five
days. Teamster reaches a person only after they opt in
([ADR 0026](0026-alerts-in-a-persons-chat.md)): they mint a code in `/admin/notifications` and
type it into the bot chat. Most employees never open the admin UI, and a reminder that depends on
someone having opted in reaches the people who need it least.

The bot can message a person only once its Teams app is installed for them. Nothing in Teamster
installs it. Three ways to get it installed for everyone exist:

* **A Teams app setup policy** in the Teams admin center. It needs no permission for Teamster, but
  it is invisible to Teamster, and a tenant may not want every app pinned.
* **Graph, per user:** `POST /users/{id}/teamwork/installedApps` with
  `TeamsAppInstallation.ReadWriteForUser.All`. Teamster can then do it, show progress and repeat
  it for people who join later.
* **The Bot Connector alone** cannot install anything. `POST /v3/conversations` does open or find
  a 1:1 chat when the app is already installed, however it got there.

[ADR 0045](0045-channel-delivery-through-the-bot.md) kept Graph for reading only. An install is a
write.

## Decision

We will install the Teams app for every enabled member of the tenant when `bot.global-install` is
on.

* **Who:** users with `accountEnabled eq true and userType eq 'Member'`. Guests and disabled
  accounts are left out.
* **How:** through Graph, on the existing Graph registration. The app is looked up in the
  organization catalog by `bot.app-id`, then by `bot.client-id`, unless `bot.catalog-app-id`
  names it. Only an org-published app can be installed this way.
* **A setup policy is tolerated.** Every person is first tried with the Bot Connector's
  `CreatePersonalConversation`. If that succeeds, the app is already there and nothing is written.
  Without the write permission, Graph's 403 skips the install and chat discovery still runs.
* **When:** an admin starts a run from a button on a new `/admin/people` page. A reconcile repeats
  it every `bot.reconcile-interval` for new joiners and for people who removed the app. A removal
  event from Teams marks the person due for the next run at once.
* **Where the state lives:** a new `directory_users` table, keyed by Entra object id, separate from
  `recipients` (a later ADR records why). A `directory_runs` table records each run's progress and
  is its lease across replicas: a unique index admits one active run, and a replica takes over a
  run whose heartbeat went stale.
* **Nobody opts out while it is on.** The chat commands `/unlink`, `stop` and `unsubscribe` and the
  notifications page's controls are disabled. A later ADR amends ADRs 0028 and 0032 with the
  details.

This amends ADR 0045: Graph now also installs the app for users. It still posts nothing.

Alternatives considered:

* **A setup policy only.** No write permission, but nothing to see or trigger from Teamster, and
  the reminder use case fails silently for anyone the policy misses. Tolerated rather than chosen.
* **Installing lazily, the first time a message names someone.** It spreads the Graph load, but
  the first message to each person waits on an install, and a burst of reminders becomes a burst
  of installs. Kept as a bounded fallback (`bot.inline-install-budget`), not the main path.
* **An Entra group as the scope.** Smaller, but IT's ask is everyone. A group filter can be added
  later without changing this model.

## Consequences

* The Graph registration needs `User.Read.All`, `TeamsAppInstallation.ReadWriteForUser.All` and,
  unless `bot.catalog-app-id` is set, `AppCatalog.Read.All`. The bot registration needs nothing new.
* The first run on a large tenant takes hours, because Graph throttles installs. The run is
  resumable and the page shows its progress.
* Each install sends Teamster an inbound `conversationUpdate`, so a run is also a burst on
  `/bot/messages`.
* `directory_users` holds the name, UPN and mail of every employee. It keeps only those fields,
  purges people who left after 30 days and is never exported.
* Reinstalling for someone who removed the app is IT's decision, made by turning the setting on.
  The README says so.
* Whether the Bot Connector accepts an Entra object id as the member of a new conversation, and
  whether the public service URL works before any activity has arrived, still has to be confirmed
  against a real tenant.
* The schema grows by two tables. Both are additive, so the previous release runs against it.
