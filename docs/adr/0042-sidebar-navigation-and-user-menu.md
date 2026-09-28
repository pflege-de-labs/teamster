# 0042. Move page links to a sidebar and account controls to a user menu

* Status: Accepted
* Date: 2026-09-28

## Context

The header carried the logo, the language picker, a link per page and the signed-in user with a
sign-out button. There are five pages now, and each translation makes the row wider, so the header
wrapped on ordinary screens. The role line under the user's name joined every value in the role
claim. For a Keycloak user that includes `offline_access`, `uma_authorization` and
`default-roles-<realm>`. It was untranslated and mostly noise.

Two constraints from earlier decisions still apply. Signing out and choosing a language must work
without JavaScript ([ADR 0008](0008-templ-tailwind-admin-ui.md),
[ADR 0015](0015-localizable-ui.md)). There is no frontend framework and no bundler.

## Decision

We will list the pages in a left sidebar and put language, user info and sign-out in a drop-down
under the user's name.

* **Sidebar.** It is rendered open, so it works without JavaScript. `web/nav.js` reveals a burger
  button that folds the sidebar away and remembers the choice in `localStorage`. It also marks the
  current page.
* **User menu.** It is a `<details>` element. The browser opens and closes it without a script, and
  the arrow turns with Tailwind's `group-open:`. `nav.js` only adds closing it on an outside click or
  Escape. Its entries are a link to `/admin/userinfo`, the languages and the existing sign-out form.
  Each language is a submit button showing its flag (the Union Jack for English, a globe for
  **Browser default**), with the language's name as the image's alt text. A `<select>` cannot
  hold an image or alt text. A language added through `ui.locale-dir` has no flag, so its button
  shows the name.
* **Role line.** It shows the highest built-in role, translated (`authz.Highest`). Roles nest
  (admin ⊃ editor ⊃ viewer), so one name says what the user may do under the shipped policies.
  Every role, including a deployment's own, is listed on the user info page instead
  ([ADR 0043](0043-session-keeps-sign-in-identity.md)).

The login page keeps the language picker in its header, because it has no user to open a menu for.

Alternatives considered:

* A menu built in JavaScript, which would make signing out depend on a script.
* A CSS checkbox hack for the sidebar, which gives no persistent state and no correct
  `aria-expanded`.
* Listing every role translated, which puts `offline_access` back.

## Consequences

The header fits in one row regardless of language. Three more clicks' worth of UI now sits behind a
disclosure, which is the trade for that. A deployment role such as `auditor` no longer shows in the
header. Users who rely on it must open the user info page. Without JavaScript the sidebar cannot be
folded, and the menu stays open until its summary is clicked again.
