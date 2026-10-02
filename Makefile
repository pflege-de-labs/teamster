.PHONY: serve build lint versions

# Builds as the dev version, the way the pages branch is published.
serve:
	hugo server --buildDrafts --baseURL http://localhost:1313/teamster/dev/ --appendPort=false

build:
	hugo --gc --minify --panicOnWarning

lint:
	markdownlint-cli2 "**/*.md" "#public" "#resources"
	scripts/check-translations.sh

# Writes versions.json into static/ so the menu can be tried locally; do not commit it.
versions:
	scripts/versions.sh static
