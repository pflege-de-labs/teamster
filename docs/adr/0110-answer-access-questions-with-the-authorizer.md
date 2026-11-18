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
role reaches are summarised once per role instead of listed. Records keep the grant row of the page's
own principal (`Direct`), which is the only one the page may edit.

Computing it from the grants table lost: it would re-implement inheritance and drift from Cedar.

## Consequences

* The cost is records times actions `AllowFor` calls plus one `ListPermissions`; it is paid per page
  view, and fine at the sizes teamster has. Revisit with a cache on the snapshot generation.
* The same function serves the user page and `/admin/access`.
* Reasons are localised where they are built.
