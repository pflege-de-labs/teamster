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

Roles come from the same claim the sign-in check reads. `auth.admin-values`, `auth.editor-values`
and `auth.viewer-values` map claim values to roles, most privileged first, and a value named in any
of them may sign in whether or not `auth.allowed` repeats it. **A deployment that configures none of
them keeps what it had: whoever may sign in administers.** A user who is allowed in but named by no
role list gets `viewer` — least privilege, not most. The local credentials administer: they are the
way back in when the provider is wrong, and a bootstrap login that could not fix the configuration
would be useless.

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
