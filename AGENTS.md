# Teamster user documentation

This branch is the source of the Teamster user documentation site,
<https://pflege-de-labs.github.io/teamster/>. It holds no Go code. The application, its
architecture, ADRs and contribution rules live on `main`; the decision behind this site is
[ADR 0080](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0080-publish-user-docs-as-a-versioned-site.md).

## Branches and versions

| Branch | Documents | Published at |
| --- | --- | --- |
| `pages` | unreleased `main` | `/teamster/dev/` |
| `pages-vX.Y` | release `vX.Y.*` | `/teamster/vX.Y/` |
| `gh-pages` | built output, written by CI only | — |

* A user-visible change on `main` gets a pull request here, against `pages`.
* A fix that also applies to a released version is cherry-picked onto its `pages-vX.Y`.
* Merge the docs for everything in a release before it is tagged. Once `vX.Y.0` is published, the
  release workflow on `main` creates `pages-vX.Y` from `pages` and deploys it as the latest
  version, so whatever `pages` holds at that moment becomes that version's documentation.
* If the workflow skipped the branch (it warns when a newer minor already exists), create it by
  hand from the right commit and run `docs-deploy` on it with `gh workflow run`.
* A pull request is previewed at `/teamster/pr-preview/pr-N/` and the bot comments the link.

## Layout

* `content/docs/` — the documentation in English (`name.md`) and German (`name.de.md`), sorted by
  [Diátaxis](https://diataxis.fr/):
  `getting-started/` (tutorials), `guides/` (how-to), `reference/`, `concepts/` (explanation).
* `layouts/` — overrides of the [Hextra](https://imfing.github.io/hextra/) theme. Keep them few;
  each is a copy that a theme update does not reach.
* `assets/js/versions.js` — fills the version menu and banner from `versions.json`.
* `i18n/` — labels of the version menu and banner, per language. `GLOSSARY.de.md` — the German
  terms.
* `scripts/check-translations.sh` — fails when a page lacks its counterpart in either language.
* `scripts/versions.sh` — writes `versions.json` and the root redirect from the `pages-v*`
  branches.

## Writing

The site is English and German. English is the source and is served at the version root; German
is served under `de/`.

* `docs-writer` (`.claude/agents/docs-writer.md`) writes and revises the English pages.
* `docs-translator` (`.claude/agents/docs-translator.md`) writes the German page for each of them
  and carries later English changes into it.

Use both, in that order, in the same pull request: a pull request that changes an English page
changes its German page too. Their rules apply to anyone writing here. Each docs pull request
links the pull request on `main` whose behaviour it documents, and that one links back; `main`'s
definition of done requires it.

## Definition of done

* `make build` passes: `--panicOnWarning` turns a broken `ref` or shortcode into a failure.
* `make lint` is clean: markdownlint with `main`'s rules, and every page in both languages.
* The German pages say what the English pages say, in the terms of `GLOSSARY.de.md` and the
  admin UI's German labels.
* Every flag, key, path and default on a page matches the code at the version the branch documents.
* Commits follow [Conventional Commits](https://www.conventionalcommits.org), usually `docs:`.

## Local preview

```bash
make serve          # http://localhost:1313/teamster/dev/
```

Requires Hugo (version in `.github/workflows/docs-deploy.yml`) and Go for the theme module.
