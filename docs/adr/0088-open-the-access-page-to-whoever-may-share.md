# 0088. Open the access page to whoever may share a record

* Status: Accepted
* Date: 2026-10-05

## Context

Record permissions ([ADR 0075](0075-own-and-share-records-through-generated-policies.md)) were
edited only in a panel at the bottom of each record's edit page. Granting a group access to ten
templates took ten page loads, the principal was typed into a free-text field, and nobody could
see what had been shared with them except as raw policy text. `/admin/access`
([ADR 0076](0076-webhook-permissions-and-access-overviews.md)) is the one page that spans records,
but it asks `administer` on `Access`, so the people who actually share records — owners — could not
open it.

## Decision

`/admin/access` stays one page and opens to anyone who holds a grant. What they see depends on what
they may do:

* Everyone: a picker of the resource classes and records they may `share`, a batch form that gives
  one principal the same actions on every ticked record, and the grants on records they may share,
  with names and links.
* Admins (`administer` on `Access`) additionally: webhook and message levels, "Who can?", and the
  policies in force. These sections are decided in the handler, not by the path.

A batch is all or nothing in one transaction. Each record runs the same `grantRefusal` as a single
grant, so nobody grants more than they hold, and the first refusal names its record. No new
authorization rule is introduced.

The principal is chosen from the users, groups, provider groups and roles known here. A local group
must exist. A user or provider group that has not signed in yet is accepted with a visible warning,
because granting before someone's first sign-in is legitimate; `/api/sharing` is unchanged.

`/admin/me` lists the records shared with the viewer, directly or through a group, provider group
or role, with links, and summarises what each role allows on whole collections instead of
enumerating records.

Alternatives: a separate page next to `/admin/access` (two overviews of the same grants), and
rejecting unknown principals outright (breaks granting ahead of a first sign-in and changes the
API).

## Consequences

* Share holders get a way across records; sharing ten records is one form.
* The nav offers the page to anyone holding a grant, which is an approximation of "may share
  something": a holder with no shareable records gets the page with a note, not a 403.
* `recordPaths` gains `/admin/access` and `/admin/sharing/batch`, so both are covered by the
  deferred-check witness.
* Collection-level `create` grants are still not offered; that is a separate decision.
* ADR 0076's statement that `/admin/access` is the admins' overview now holds for its admin
  sections only.
