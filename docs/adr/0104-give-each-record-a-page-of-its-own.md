# 0104. Give each record a page of its own

* Status: Accepted
* Date: 2026-11-10

## Context

[ADR 0103](0103-group-the-admin-sidebar-and-give-each-resource-a-page.md) gave every resource a
page, but each still stacks a create/edit form, the full list and a sharing panel. An admin cannot
look at one record without a form in the way, and editing jumps to a crowded page. Links already
posted into Teams, `/admin/<resource>?edit=<id>`, must keep working. Nine resources need the same
treatment, which is too much for one change.

## Decision

We will give each resource a list, a record and a new page, destinations first. This builds on ADR
0103, which stands.

* **A shared frame.** `listPage`, `recordPage` and `newPage` in `views/record.templ` hold the
  breadcrumb, actions and layout; each resource has its own view structs and handlers.
* **URLs.** `GET /admin/destinations` lists, `/admin/destinations/new` is the create form and
  `/admin/destinations/{id}` is the read-only record. Literal `POST` paths (`/delete`, `/default`)
  coexist with `{id}` in `ServeMux`.
* **Edit in place.** The record page renders a view part and an edit part; `?edit` or a failed save
  shows the edit part. `web/record.js` swaps them without a reload, keeps `?edit` in the address
  bar with `replaceState`, and warns before leaving with unsaved changes. Without script, Edit and
  Cancel are plain links.
* **A failed save re-renders** the new or record page in edit mode with the typed values and the
  error, status 422, no redirect; an unexpected store error is a 500 with a generic message. A
  successful save answers 303 to the record, a delete 303 to the list.
* **404 for unreadable records.** A record the viewer may not read, or that does not exist, answers
  404, so the page does not confirm it exists. This holds for an update POST as well. The create
  page answers 403 without the create right.
* **Old links.** `/admin/destinations?edit=<id>` and `/admin?edit=destinations&id=<id>` answer 301
  to `/admin/destinations/<id>?edit`.
* **One pull request per resource**, in the order templates, webhooks, routes, groups, access
  tokens, recipients, users, delivery permissions.

Alternatives considered:

* One `views.Page` with a mode field. It already carries the lists of five resources, so every
  page would load everything.
* A generic Go resource interface behind shared handlers. The resources differ too much (preview,
  one-time secrets, no create for users) for it to pay off.
* A separate `/edit` URL. It costs a page load per edit and makes a failed save a third place to
  render the form.

## Consequences

A record can be linked, read without a form and edited in place. Until every resource is
converted, `?edit=` stays the edit link of the others. A failed save is a POST response, so a
reload re-posts and the browser warns. The redirects must stay as long as Teams cards carrying the
old links exist. Nothing changes in the schema or in what a role may do.
