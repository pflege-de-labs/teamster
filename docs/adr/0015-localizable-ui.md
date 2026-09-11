# 0015. Keep the UI's text in catalogs, not in the components

* Status: Accepted
* Date: 2026-09-11

## Context

Every string the admin UI showed was written into the templ component that showed it. Two
consequences: changing one sentence meant finding the component that held it, and a second language
was impossible, because the text and the markup were the same file.

## Decision

We will keep the text in **per-language JSON catalogs keyed by identifier**, embedded in the binary,
and look a string up where it is rendered.

`golang.org/x/text/language` does the language negotiation, which it is good at. We will **not** use
`x/text/message` and its `gotext` code generation: that pipeline wants Go source as the extraction
target and a generated catalog as the output, which fits a program printing to a terminal better than
it fits templ components. JSON files an operator can read and edit are the artefact we want, and the
lookup that reads them is twenty lines.

`go-i18n` and its TOML catalogs were the other candidate. TOML reads a little better than JSON for
long strings, which is not worth a dependency when `encoding/json` is in the standard library and the
matcher we need is in `x/text`, which this build already carries.

**The language travels in the context.** templ components take a `context.Context` already, so a
component calls `i18n.T(ctx, "key")` and nothing has to be threaded through every component's
parameters — the sort of change that gets half done.

**Values go in at `{0}`, `{1}`, not at `%s`.** A translator can reorder them, which some languages
require; the numbering says how many there are; and it keeps `T` from looking like a printf wrapper,
which `go vet` correctly complains about when the format string is a key rather than a format.
Sentences are never built by concatenation for the same reason: "Save template" is
`action.save` = `Save {0}` and `noun.template` = `template`, because German puts them the other way
round — `Vorlage speichern`.

**A missing key renders as the key.** A page saying `routes.badge_default` says what is wrong; an
empty element says nothing. A key missing from a translation falls back to English first.

**An override directory** — `ui.locale-dir` — is read after the embedded catalogs and wins, entry by
entry. That is the part that makes the text genuinely editable rather than merely centralised: an
operator can retune a sentence, or add a language this build has never carried, without waiting
for a
release. A file there is named for its language, checked against the shape of a language tag rather
than by `language.Parse` alone, which reads `not-a-language` as a valid tag with extensions.

English is the source. German ships with it, so the second language is real rather than theoretical,
and a test asserts that every catalog carries every source key and keeps every placeholder.

The card palette names its buttons with catalog keys rather than text, so
`internal/cards` describes the fragments and the catalogs say what to call them. The words the Team
and channel pickers build themselves with travel to the browser as data attributes on the fields the
server rendered: that script has no catalog and no language, and shipping it one would be a second
place for the text to live.

**What is not translated:** webhook responses, API errors and log lines. They are read by machines,
and by whoever is reading a log at three in the morning; a translated error is harder to search for.
Alert templates are the operator's own text already.

## Consequences

Wording changes happen in one file. Adding a language is a file, and adding one *without* a release
is a file in a directory.

Every component that shows text now takes the context it already had, and the view helpers that
build a phrase — the role label, the grant scope, the submit button, the selector summary — take one
too. That is the cost: a helper that returns a sentence cannot be a pure function of its arguments
any more.

`misspell` reads every string as English, so it is turned off for `internal/i18n`. That is a narrow
hole: it covers the catalogs and their tests, and nothing else.

The catalogs are not extracted automatically. Adding a string to a component means adding a key to
`en.json` and `de.json`, and the completeness test is what catches forgetting the second one. An
extraction tool would remove that step, and can be added later without changing the format.
