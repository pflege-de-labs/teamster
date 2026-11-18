# 0111. Look up principals past the first page of users

* Status: Accepted
* Date: 2026-11-18

## Context

Pages load the first 200 users (`usersPageSize`) for two jobs: the picker's options and the names
in grant, level and member lists. With more users, a grant to the 201st showed its subject, and the
picker could not offer them; ADR 0109 left them to "Someone not listed".

## Decision

We will name users from `ListReferencedUsers`: every user a grant or a group membership names. Those
are the only users a list can show, so names no longer depend on how many have signed in.

We will keep the first page as the picker's options and mark the select with `data-search-url`
when the page was full. Typing two or more characters then asks `GET /admin/principals?q=` for up to
20 matching users, which the script adds as options. The endpoint answers admins, editors and anyone
holding a grant: the people a picker is shown to.

Prefetching further pages as the list scrolls lost: nobody scrolls to the five-hundredth user, they
type part of the name, and paging would load the directory into every page.

## Consequences

* Names resolve for any number of users, with one query per page instead of one per row.
* A large directory is searchable from every picker; without script, users past the first page
  still need "Someone not listed".
* A reader a record was shared with, but who may not share it, can search user names and emails.
  The sharing panel already lists the first page to anyone who may share.
* Provider groups and custom roles in the picker still come from the first page of users.
