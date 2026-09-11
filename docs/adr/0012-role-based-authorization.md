# 0012. Authorize with Cedar, evaluated in-process

* Status: Accepted
* Date: 2026-09-11

## Context

An authenticated session could do anything. [ADR 0009](0009-admin-authentication.md) decided that a
claim value grants access; access was all there was to grant. Anyone an operator wanted to let read
the routing configuration could also delete every template in it.

What is wanted is `admin`, `editor` and `viewer`, and eventually a scope: which Teams and channels
an editor may deliver to. That is a policy model — subjects, actions, resources, group membership —
and hand-rolled versions of it spread `if role == …` across every handler, where nobody can read
the rules as a whole and no test covers the combinations.

The first constraint in [the roadmap](../roadmap.md) is one static binary with no separate
deployment, which rules out the obvious answer of running an authorization service.

## Decision

We will use [`cedar-policy/cedar-go`](https://github.com/cedar-policy/cedar-go), Cedar's Go
implementation, as a library. The authorizer runs in-process against policies embedded in the
binary: no second process, no second database.

The rules live in `internal/authz/policies.cedar`, three policies long, readable without reading Go:
an admin may take any action, an editor may `view` and `edit`, a viewer may `view`. Roles nest
through Cedar's entity parents — `Role::"admin"` has `Role::"editor"` as a parent, which has
`Role::"viewer"` — so the policies do not repeat themselves and `principal in Role::"viewer"` holds
for everyone.

`Action::"administer"` exists and is granted to admins only. Nothing uses it yet; it is what the
per-Team grants of the next change will hang off.

Enforcement is one middleware, `httpserver.authorize`, wrapping the admin mux. It maps a request to
a resource type by path and to an action by method, with two deliberate exceptions: `POST`s to
`/api/templates/preview` and `/api/routing/match` count as `view`, because they answer a question
and change nothing. Refusals are `403` naming the role and the resource, not `404`.

**Roles are read from the claim by name, all of them.** A provider role called `admin`, `editor` or
`viewer` is that role here; one called `auditor` is `auditor`, and every role a user carries becomes
a parent of the Cedar principal. There is no mapping to configure, because a mapping between two
sets of identical names is a configuration file that can only be wrong — and passing the unknown
ones through costs nothing, since a role no policy mentions grants nothing. It buys a deployment the
ability to define a role and a policy for it without patching the claim handling.

`admin`, `editor` and `viewer` are therefore a floor rather than the whole set: the shipped policies
define them, the UI asks about them, and `auth.default-role` is one of them.

`auth.allowed` is gone with it. An allow-list for signing in made sense when signing in was the only
thing to grant; now the role is, and a user the claim names nothing for holds no role and can do
nothing. `auth.default-role` says what such a user gets: `viewer` to let everyone the provider
authenticates read the configuration, or empty for no access at all. It fills in for a missing
**Teamster** role rather than an empty claim, because Keycloak hands `offline_access` to everyone
and a claim is therefore almost never empty — a default that waited for one would never apply.

This moves a decision to the provider: whether everyone in a realm can reach this client is now the
client's configuration there, not an allow-list here. That is where it belongs — Keycloak already
has the concept — but it is a change in where a deployment is locked down, and empty is the safe
`default-role` when the client is open to the realm.

A user with no role signs in and **is told so**, on a page naming the roles to ask an administrator
for. The alternative — failing the login — reports a permissions problem as an authentication
problem, which sends the operator to the wrong place. `RoleNone` is therefore a role rather than an
empty string: "no permissions" is a state the UI can explain.

The local credentials administer: they are the way back in when the provider is wrong, and a
bootstrap login that could not fix the configuration would be useless.

The session carries its role, so a role change at the provider takes effect at the next sign-in
rather than mid-session. A session written before this change carries no role and is read as an
admin: it already had that access, and demoting an operator halfway through their shift is a worse
failure than honouring a session that expires within the day anyway. A role this build does not
recognise is refused everything.

`/api` now accepts **either** a session or the local credentials. Before this, it took only basic
auth, which meant a browser signed in through the provider could not load the pickers, the preview
or the routing graph — the UI's own fetches were unauthenticated. A cookie travels with a
cross-site request, so a state-changing `/api` call authenticated by a session must also pass the
same origin check the form posts use; basic auth needs no such check, because those credentials are
sent deliberately.

`NewServer` now returns an error. A policy file that does not parse must stop the process, not leave
every check to whatever the zero value decides.

## Scoping a role to Teams and channels

A `grants` row names a role, a Team and optionally a channel. The scope those grants describe
becomes attributes on the principal, and three scoped actions — `deliver`, `viewChannel`,
`viewTeam` — are decided against them; a channel entity has its Team as a parent, so granting a Team
reaches its channels without expanding the channel list, which would otherwise make every permission
check depend on a Graph call.

**A role no grant names is unrestricted.** The alternative — no grant means no access — would make
this change break every installation on upgrade, and would put a migration between an operator and
their own configuration. The cost is that narrowing is opt-in, which the admin page says in as many
words.

Grants are keyed by role rather than by user, which is the same decision as reading roles by name:
"the payments editors" is a role in the provider and a grant here, and there is no second notion
of a group to keep in step.

Deciding who may deliver where is `Action::"administer"`, so an editor cannot widen their own scope.

## Consequences

The admin UI hides what a role may not do — the three forms, the edit links and the delete buttons —
and says why, so a viewer sees a page of lists and one sentence rather than a page of controls that
all fail. Hiding is not enforcing: the middleware refuses the post regardless, and a test asserts
both halves.

Adding an action or a resource is now editing a policy file and a switch statement, not auditing
every handler. Adding the per-Team grants means adding entities and one policy, not rewriting this.

It supersedes part of ADR 0009: membership no longer grants access, it grants a role.

Cedar is a new dependency — a policy language and evaluator for what is currently three rules. That
is the cost of the model being data rather than control flow, and it buys the next milestone the
part that would have been genuinely awkward by hand: grants scoped to a Team, inherited by its
channels.
