# 0081. Publish the user documentation in English and German

* Status: Accepted
* Date: 2026-10-02

## Context

The admin UI already speaks English and German (`internal/i18n/locales`). Its users are largely
German-speaking operators, and the documentation site
([ADR 0080](0080-publish-user-docs-as-a-versioned-site.md)) is English only. A second language
doubles every page that has to stay true to the code, and translations drift silently: a German
page that still describes last release's behaviour reads just as confidently as a correct one.

## Decision

* **Hugo's multilingual mode, one site per version.** English stays at the version root, so every
  link published so far keeps working; German is served under `de/` of the same version. Hextra
  provides the language switch and a German search index.
* **English is the source.** Pages are written in English and translated, as `name.de.md` next to
  `name.md`. Separate content directories per language lost: they put a page and its translation
  far apart, which is how one gets changed without the other.
* **Every page exists in both languages, in the same pull request.** CI fails when a page lacks its
  counterpart. A fallback to English for missing German pages lost: it hides exactly the drift this
  decision is about.
* **German headings keep the English anchors** (`{#english-id}`), so links into a page are the same
  in both languages and a translation cannot break them.
* **The German pages use the admin UI's German labels** and a glossary on the `pages` branch, and
  address the reader as "Sie", as the UI does.
* **A `docs-translator` agent** carries English changes into German, next to the `docs-writer`
  agent that writes the English pages.

## Consequences

* Every documentation change costs a translation. The definition of done on `main` asks for both
  languages.
* A change to the UI's German labels can make German pages wrong without changing any English
  page.
* Versions published before this decision have no German pages; `pages-v0.11` gets them as part of
  this change, older versions do not exist.
