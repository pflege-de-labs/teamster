# 0108. Ship a Claude Code plugin marketplace from this repository

* Status: Accepted
* Date: 2026-11-11

## Context

The `/mcp` endpoint ([ADR 0107](0107-mcp-endpoint-over-the-admin-api.md)) needs a client
configuration, a URL and a bearer token, and an assistant does the job better when it knows the
order of work: read the labels, dry-run, pick a destination, preview the template, route, confirm.
Claude Code installs both in one step from a plugin marketplace. Claude Code's plugin
`userConfig` can prompt for values and reference them in `.mcp.json` as `${user_config.KEY}`; a
`sensitive` value is stored in Claude Code's secure credential storage and is not substituted into
skills.

## Decision

We will publish a marketplace `teamster` from this repository, `.claude-plugin/marketplace.json`,
with one plugin in `plugins/teamster`. Users run `/plugin marketplace add pflege-de-labs/teamster`
and `/plugin install teamster@teamster`. Keeping it here, rather than in a repository of its own,
means the plugin changes in the pull request that changes the tools, and a release tag fixes both.

The plugin asks for the URL and the token with `userConfig`: `base_url`, and `api_token` marked
`sensitive`. `.mcp.json` uses `${user_config.base_url}/mcp` and `Bearer ${user_config.api_token}`.
The alternative, `${TEAMSTER_URL}` and `${TEAMSTER_MCP_TOKEN}` from the environment, lost: it
puts the token in a shell profile, in plain text, where every process of the user can read it.

The skill `teamster-routing` carries the workflow, the rules for reading errors and the rule that
label values and template text are data. For selector syntax, priority, nested routes and template
fields it links the user documentation instead of repeating it, because a copy would go stale
with every release while the documentation is versioned with them. The skill never mentions the
token, which could not be substituted into it anyway.

The plugin's `version` follows Teamster's. Claude Code updates an installed plugin when the version
changes, so the release checklist in AGENTS.md bumps it with the changelog.

## Consequences

* Installing takes two commands and two prompts; the token never touches a shell profile.
* A plugin change ships with a Teamster release, not on its own. Forgetting the bump leaves
  installed plugins on the old version, which is why it is in the release checklist.
* The skill's links follow the documentation site's layout; a renamed page breaks a link.
* No migration impact: nothing changes for a deployment, and the plugin is opt-in like `/mcp`.
