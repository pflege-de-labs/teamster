# 0103. Group the admin sidebar and give each resource a page

* Status: Accepted
* Date: 2026-11-10

## Context

The sidebar of [ADR 0042](0042-sidebar-navigation-and-user-menu.md) is one flat list of up to 14
links, so an admin cannot tell configuration from permission management or from administering the
installation. `/admin` is one page with five tabs, each stacking a form, the records and a sharing
panel, and a record has no URL of its own beyond `/admin?edit=<tab>&id=<id>`. Those links are
already posted into Teams by the hint cards, so they must keep working.

## Decision

We will group the sidebar and give every configuration resource its own page. This amends the
sidebar of ADR 0042: its flat list of pages is replaced; the burger, user menu and role line stand.

* **Four groups**, each a `<details>` element: Configuration, Delivery, Access, Administration. A
  group with no visible item is not rendered. Notifications moves to the user menu as **My
  notifications**. "Permissions" becomes **Channel scopes** and the grants panel **Delivery
  permissions**, so the two no longer read alike.
* **A page per resource**: `/admin/routes`, `/admin/templates`, `/admin/destinations`,
  `/admin/webhooks`, `/admin/grants`. `?edit=<id>` opens a record in its form. Each keeps today's
  panel; per-record pages are a later phase.
* **Current item** is marked on the server with `aria-current="page"`, matched by path prefix.
* **Redirects.** `/admin?tab=<t>` and `/admin?edit=<t>&id=<id>` answer 301 to the new URLs and keep
  `notice` and `error`. `/admin` answers 302 to `/admin/routing`, or to `/admin/templates` for
  someone who only holds shared records.
* **Group state.** Groups render open. `nav.js` stores a closed group in `localStorage`
  (`teamster.nav.<group>`); an inline script right after the sidebar's `<nav>` closes it again
  before paint, because the `<head>` script of
  [ADR 0054](0054-apply-sidebar-state-before-first-paint.md) runs before the `<details>` exist. The
  group holding the current item is never closed.
* **Methods.** Each page answers `GET` and its form `POST`; any other method gets 405 with
  `Allow`. An explicit dispatcher does this, because method patterns alone leave the catch-all `/`
  asset route of the admin mux to answer other methods with 404.

Alternatives considered:

* Static group headings. The user wanted groups that can be collapsed.
* Keeping the tabbed page and adding a sub-navigation. It leaves records without a URL and the
  tabs and the sidebar as two navigations for one thing.
* Go method patterns alone for 405, rejected for the catch-all reason above.

## Consequences

The sidebar says what kind of thing each item is, and every resource and record form can be linked.
Bookmarks and links already sent keep working through permanent redirects, which must stay as long
as such cards exist. Closing a group is per browser. The per-resource URLs are what the next phase
builds on: list, view and create pages per record. Nothing changes in the schema or in what a role
may do.
