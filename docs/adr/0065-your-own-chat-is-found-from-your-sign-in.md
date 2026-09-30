# 0065. Your own chat is found from your sign-in when the app is installed for everyone

* Status: Accepted
* Date: 2026-09-30

## Context

A route may deliver to "yourself": the admin-UI user's own chat, a recipient bound to their session's
subject ([ADR 0047](0047-a-route-targets-a-channel-or-yourself.md)). That binding came from a link
code, minted in the admin UI and typed into the bot chat
([ADR 0026](0026-alerts-in-a-persons-chat.md)). The code proved that the Teams account belonged to
whoever held the session.

With the app installed for everyone ([ADR 0059](0059-install-the-teams-app-for-every-member.md)),
everyone already has a chat, and IT asked that nobody has to subscribe. The sign-in can make the same
proof, provided it names the Entra account. The session subject cannot: Entra's `sub` is pairwise per
application, and Keycloak's is its own.

## Decision

With `bot.global-install` on, we will bind a user's own chat from their sign-in and stop using link
codes.

* The session keeps three more fields in its identity: `object_id` from `auth.object-id-claim`
  (default `oid`), looked up in the id token, userinfo and the access token like the role claim;
  `username` from `preferred_username`; and `email_verified`.
* **Matching**, in order:
  1. the object id, exactly;
  2. the username, when it equals the directory user's UPN (ignoring case);
  3. the email, only when the provider says it is verified.

  The directory also matches mail aliases, so a username has to be the UPN itself. An unverified
  email is anybody's to claim. A local login never binds: it says nothing about a Teams account.
* **When.** After the callback, best effort. Also when the notifications page finds no recipient
  for the session. Neither installs the app. When the user has no chat yet, the page offers
  **Set up my chat now**, which installs it for them and binds.
* **What.** A recipient keyed by the session's subject, as a redeemed code would have made it. Routes
  to "yourself" and `/status` keep working unchanged.
* **Keeping up.** When the bot records a person's chat, from an install event or any later
  activity, every recipient with their object id moves to it. A new index on
  `recipients(aad_object_id)` serves that.
* **Codes go.** Minting and cancelling are refused on the page (`codes_not_needed`), the API answers
  409, and a code typed into a managed chat gets a reply saying it is not needed.

Alternatives considered:

* **Keep link codes alongside.** Two ways to the same binding, and the code way asks people to do
  something IT said they should not have to.
* **Bind by display name or email without verification.** A guessable claim would let someone bind
  another person's chat to their own subject, and then route to it.

## Consequences

* With Keycloak brokering Entra, the realm needs a mapper that passes the Entra `oid` through as a
  claim. Without it, the username or a verified email still matches in most tenants.
  `docs/keycloak.md` describes the mapper.
* An admin can still remove a recipient. The next sign-in binds it again.
* Amends ADR 0026's linking flow for deployments with global install on. Nothing changes when it is
  off.
* Schema: one index, SQLite `0022` and Postgres `0019`. Additive.
