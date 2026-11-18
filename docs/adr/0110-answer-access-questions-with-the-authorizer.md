# 0110. Answer access questions with the authorizer

* Status: Accepted
* Date: 2026-11-18

## Context

A group's page must say what the group may do: records it reaches directly, through a parent group
or a role, and its webhook and message levels. Reading the `permissions` table gives only the rows
naming the group; it misses parents, roles and the hierarchy of actions, and would duplicate the
rules Cedar already decides.

## Decision

We will ask the authorizer. `authz.Principal.Groups` lets a caller name a synthetic principal: an
empty `Subject` with `Groups` set is a member of those groups and nothing else, so the answer is
what the group itself reaches. `whatMay` enumerates every record, asks `AllowFor` per action and
takes the reasons from `ExplainedPolicy.Grant` and `.Role`. The role is read from the principal
scope only, so a role the viewer holds does not leak into the group's answer. Rows that only a base
role reaches are summarised once per role instead of listed. Records keep the grant row of the
page's own principal (`Direct`), which is the only one the page may edit. The answer names only
records the viewer may read, and only admins see the levels, so a group page does not disclose
what its reader could not open.

The access check on `/admin/access` has two modes on the same footing. "What may ... do?" takes a
user, group, provider group or role and runs `whatMay`. "Who can ...?" takes an action and a record
and asks the authorizer (`whoCan`) for every signed-in, non-disabled user, read page by page
(`ListUsersPage`, keyset on subject); a "Narrow to" picker limits it to one principal. Both modes
build their reasons with `reasonsFor`, so a row reads the same in either. "create" is not offered: the
resource list holds records, not collections.

Computing it from the grants table lost: it would re-implement inheritance and drift from Cedar.

## Consequences

* The cost is records times actions `AllowFor` calls plus one `ListPermissions`; it is paid per page
  view, and fine at the sizes teamster has. Revisit with a cache on the snapshot generation.
* The same function serves the group page and `/admin/access`; `/admin/me` keeps `grantedTo`.
* "Who can?" costs one `AllowFor` call per signed-in user and action, however many users there are.
* Reasons are localised where they are built.
