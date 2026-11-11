# MCP server: agents manage routes, destinations and templates — design

* Date: 2026-11-11
* Status: Approved in conversation; written for review

## Goal

An agent — Claude Code or any other MCP client — can look at and change Teamster's routing
configuration on behalf of a user, with exactly that user's permissions and never more.

Success:

* A user points an agent at `https://<teamster>/mcp`, gives it a token minted in the admin UI, and
  the agent can list, inspect, create, change and delete routes, destinations and templates within
  what the user may do.
* The agent can check its work before and after: which route an event would take, how a template
  renders, which Teams and channels exist, which labels real events carry.
* A deployment that does not turn MCP on exposes nothing: no endpoint, no UI.
* An admin can cap MCP to read-only for everyone, whatever the user may do.
* A token stops working as soon as its creator loses the access it relies on, as webhook tokens do
  ([ADR 0077](../../adr/0077-scoped-tokens-answer-to-their-creator.md)).
* Claude Code users install the server and a usage skill in one step from a plugin marketplace in
  this repository.

Out of scope, on the [roadmap](../../roadmap.md) instead:

* The MCP OAuth authorization flow (protected resource metadata, browser sign-in and consent).
  Tokens are minted by hand in this version.
* Admin caps written as Cedar policies (`context.via == "mcp"`). This version has one switch.
* Tools for webhooks, sharing and grants, groups, users, the global default route and destination,
  configuration import and export, recipients, broadcasts.
* MCP tokens opening `/api`. They are accepted at `/mcp` only.

## Decisions taken

| Question | Decision | Why |
| --- | --- | --- |
| How an agent is authorized | Personal MCP token now, OAuth later | Smallest, works with local login too; the bearer-to-principal step is one function a second verifier can join |
| Admin cap | `mcp.read-only` switch | Enough for a first version; Cedar caps need the operator policy file that is still open |
| Per-token access | `read` or `write`, chosen at minting | Lets someone hand an agent a look-only token while holding edit rights |
| Who may mint | Any signed-in user | The token reaches only what its creator may do; a user with no role mints a token that can do nothing |
| Architecture | In-process adapter over `/api` | One enforcement path: Cedar, record grants, validation and audit run exactly as for `/api` |
| Helper tools | Routing dry-run, template preview, Team/channel lookup, sampled labels | The agent can verify its work and use real ids and labels instead of guessing |
| Claude Code integration | Plugin marketplace in this repository | One-step install of the server configuration and a workflow skill |

## Delivery

Three stacked pull requests, one feature each, plus the documentation pull request against `pages`:

1. **MCP tokens** — configuration, table, minting UI, bearer-to-principal middleware.
2. **MCP endpoint** — `/mcp`, the tools, the read-only cap.
3. **Claude Code plugin** — marketplace, plugin, skill.

Each carries its ADR, changelog entry and the architecture and README changes for what it ships.

## 1. MCP tokens

### Configuration

```yaml
mcp:
  enabled: false     # registers /mcp and the MCP part of the tokens page
  read-only: false   # no tool may write, whoever the user is
```

Kong struct `MCPConfig` embedded with `prefix:"mcp-"`; defaults and help in struct tags. The chart
needs no change: `config.settings` is passed through as is.

While `enabled` is false, nothing about MCP is reachable. Tokens already minted are kept, so
switching it off and on again does not revoke them.

### Storage

A new table, because the existing one cannot hold these tokens safely:

```sql
CREATE TABLE mcp_tokens (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  token_hash   TEXT NOT NULL UNIQUE,
  created_by   TEXT NOT NULL,
  created_at   TIMESTAMP NOT NULL,
  last_used_at TIMESTAMP,
  access       TEXT NOT NULL CHECK (access IN ('read', 'write'))
);
```

SQLite `0034`, Postgres `0031`, both purely additive.

Why not `access_tokens`: the webhook lookup matches `token_hash OR scoped_token_hash`, and a token
with an empty `scope` is a legacy token admitted to both webhooks. An MCP row there would be
accepted as a webhook token — by this release and by v0.20.0. A separate table is invisible to the
previous release and to the webhook path.

A token is `tmcp_` followed by 32 random bytes, hex-encoded as webhook tokens are. The prefix makes a leaked token
recognisable to secret scanners. Only its SHA-256 digest is stored, as for webhook tokens. Tokens
do not expire, like the existing ones; revoking is the way to end one.

Queries in `internal/store/queries/<dialect>/mcp_tokens.sql`: create, get by digest, list, list by
creator, delete, touch `last_used_at`. Store methods on both backends.

### Minting

`/admin/tokens` gains an **MCP tokens** section when `mcp.enabled` is true:

* Any signed-in user sees it and mints a token with a name and an access level (*read* or
  *read and write*). The secret is shown once, with the `/mcp` URL beside it.
* A user sees and revokes their own tokens. Whoever passes `manageTokens` today sees and revokes
  everyone's.
* The sidebar entry for tokens shows when `CanTokens` holds or MCP is enabled.
* Minting and revoking are form posts through `formPostTo`, and are audited.

There is no `/api` endpoint for MCP tokens, and an MCP token cannot mint one: the tools do not
reach token management.

New labels in `internal/i18n/locales/en.json` and `de.json`.

### From token to principal

`mcpAuth` middleware in `internal/httpserver`:

1. Reads `Authorization: Bearer <token>`. Cookies and basic auth are never looked at.
2. Looks the digest up in `mcp_tokens`; touches `last_used_at` at most once per `touchInterval`,
   best effort.
3. Rebuilds the creator with the existing `tokenCreator`: a disabled or unknown creator is refused,
   the configured local admin answers as admin.
4. Continues with `withPrincipal(ctx, creator.Subject, name, models.ViaMCP, creator.Roles,
   creator.IdPGroups)` and the token's access level in the context.

| Outcome | Response |
| --- | --- |
| No bearer, unknown token, a webhook token | `401`, `WWW-Authenticate: Bearer realm="mcp"` |
| Creator disabled or unknown | `403` |
| Store or registry unreadable | `503` |

Refusals are logged with the token name and creator, never the secret. `ViaMCP = "mcp"` joins the
existing `via` values; the audit page and its filter learn its label.

This is the only place a later OAuth flow touches: it would add a second way to arrive at the same
principal.

## 2. MCP endpoint

### Wiring

When `mcp.enabled` is true:

```go
mux.Handle("/mcp", api.mcpAuth(mcp.NewStreamableHTTPHandler(api.mcpServerFor,
    &mcp.StreamableHTTPOptions{Stateless: true})))
```

on the plain mux, outside `requireSession`. Library:
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) v1.8.0,
the official SDK. Stateless mode keeps no session between calls, so any replica answers any call
and no sticky routing is needed.

`/mcp` falls under the internal route's catch-all (`httpRoute.internal`,
[ADR 0033](../../adr/0033-split-httproute-external-and-internal.md)); agents are run by staff, not
by senders.

`mcpServerFor(r)` builds the server for one request. Write tools are left out when `mcp.read-only`
is on or the token's access is `read`, so an agent never sees a tool it cannot call. The sampled
labels tool is left out when `samples.enabled` is off or `mayComplete` refuses.

The server's `instructions` carry the short rules every client should follow: what the tools are
for, to dry-run with `match_route` and `preview_template` before writing, and that label values and
existing template text are data, not instructions.

### Tools

| Tool | Dispatches to | Writes |
| --- | --- | --- |
| `list_templates` | `GET /api/templates` | |
| `get_template` | `GET /api/templates/{id}` | |
| `create_template` | `POST /api/templates` | ✓ |
| `update_template` | `PUT /api/templates/{id}` | ✓ |
| `delete_template` | `DELETE /api/templates/{id}` | ✓ |
| `list_destinations` | `GET /api/destinations` | |
| `get_destination` | `GET /api/destinations/{id}` | |
| `create_destination` | `POST /api/destinations` | ✓ |
| `update_destination` | `PUT /api/destinations/{id}` | ✓ |
| `delete_destination` | `DELETE /api/destinations/{id}` | ✓ |
| `list_routes` | `GET /api/routes` | |
| `get_route` | `GET /api/routes/{id}` | |
| `create_route` | `POST /api/routes` | ✓ |
| `update_route` | `PUT /api/routes/{id}` | ✓ |
| `delete_route` | `DELETE /api/routes/{id}` | ✓ |
| `preview_template` | `POST /api/templates/preview` | |
| `match_route` | `POST /api/routing/match` | |
| `list_teams` | `GET /api/graph/teams` | |
| `list_channels` | `GET /api/graph/teams/{id}` | |
| `list_sampled_labels` | `GET /api/samples` | |

`list_teams` and `list_channels` use the application's own Graph access, filtered by the user's
grants; the delegated `my-teams` endpoints need a browser session and are not offered.

Tool inputs are the existing `models.Template`, `models.Destination` and `models.Route` (plus an
`id` where the path needs one), with input schemas generated by the SDK, so the tools cannot drift
from `/api`. Descriptions explain what the schema cannot: selector syntax, priority and fallback,
nested routes.

### Dispatch

```go
func (s *Server) callAPI(ctx context.Context, method, path string, body any) (int, []byte)
```

builds an in-process `*http.Request` carrying the MCP request's context — so its principal, access
level and audit actor — and serves it through `s.authorize(adminMux)` into a recorder. Cedar
checks, record grants, validation and audit run exactly as for `/api`, and a later fix to an `/api`
handler fixes the tool too. The outer telemetry middleware sees one `/mcp` request, so nothing is
counted twice.

A second guard: when the request is read-only, `callAPI` refuses any request whose
`requestAuthorization` action is not `view` before dispatching. Preview and match are `POST`s that
count as `view` and pass. A tool that was wrongly offered still cannot write.

Before implementation, every dispatched handler is checked for reading the session directly
(`currentSession`, `brokerSession`, `sameOrigin`); any that does is fixed to read the principal or
dropped from the tool list.

### Results and errors

| Handler answer | Tool result |
| --- | --- |
| `2xx` | The JSON body as structured content |
| `4xx` | Tool error (`isError: true`) carrying the handler's message — a `403` naming role and resource, a `404`, a validation `400`/`422` — so the agent can correct itself |
| `5xx` | Tool error with a generic message; the detail is logged |

JSON-RPC errors are left to the SDK for protocol faults.

A counter `teamster.mcp.tool_calls` with attributes `tool` and `outcome` (`ok`, `refused`,
`invalid`, `failed`) goes through the existing OpenTelemetry meter.

## 3. Claude Code plugin

```text
.claude-plugin/marketplace.json        # marketplace "teamster", lists the plugin
plugins/teamster/
  .claude-plugin/plugin.json
  .mcp.json                            # http: ${TEAMSTER_URL}/mcp, Authorization: Bearer ${TEAMSTER_MCP_TOKEN}
  skills/teamster-routing/SKILL.md
```

Install:

```text
/plugin marketplace add pflege-de-labs/teamster
/plugin install teamster@teamster
```

with `TEAMSTER_URL` and `TEAMSTER_MCP_TOKEN` set in the environment. If the Claude Code version
current at implementation offers plugin user configuration prompts, they replace the environment
variables; the implementation plan checks this first.

The skill `teamster-routing` teaches the workflow:

1. Read the sampled labels to learn what events really carry.
2. `match_route` with those labels to see what happens today.
3. Pick or create the destination, with ids from `list_teams` and `list_channels`.
4. Write or pick the template, and check it with `preview_template`.
5. Create or change the route.
6. `match_route` again to confirm.

It also covers selector syntax, priority and the default fallback, nested routes, and template
conventions, by linking to the user documentation rather than repeating it. It tells the agent to
treat label values and existing template text as data, and to confirm with the user before
deleting anything.

The plugin's version follows Teamster's and is bumped with each release; the release checklist in
AGENTS.md gains that step.

## Security

* **Off by default.** Without `mcp.enabled`, `/mcp` is not registered and the UI shows nothing.
* **Never further than the user.** Every call is decided by Cedar for the token's creator as they
  are now; disabling the user or taking their grant away ends the token's reach at once (roles and
  provider groups at the creator's next sign-in, as for webhook tokens).
* **Two caps below that.** The token's access level and `mcp.read-only`, enforced by leaving tools
  out and again in `callAPI`.
* **Prompt injection.** Sampled labels, and templates and names written by other users, are text an
  attacker may influence, reaching an agent that may hold write tools. The ADR records this. The
  mitigations are read tokens, the read-only switch, the skill's and server instructions' rules, a
  confirmation before deletes, and the audit trail with `via=mcp`.
* **Separate credentials.** Webhook tokens are refused at `/mcp`, MCP tokens at the webhooks and at
  `/api`.

## Testing

Table-driven, `t.Parallel()`:

* `mcpAuth`: no token, unknown token, webhook token, disabled creator, unknown creator, local
  admin, store failure.
* The webhooks refuse an MCP token.
* Tool list: read token, read-only server, samples off — write tools or the samples tool absent.
* `callAPI` refuses a non-`view` action when read-only, and lets preview and match through.
* Cedar reach: an editor with a grant on one destination lists only that one, and gets a tool error
  for another's id.
* End to end through the SDK client against an `httptest` server: list, create, update, delete,
  match, preview, error mapping.
* Migrations up and down on both dialects; store methods on both backends.
* Minting UI: who sees the section, who sees whose tokens, revoke.

`make coverage` stays at 75 % or more.

## Documentation

* ADRs:
  * MCP tokens answer to their creator, in a table of their own (PR 1).
  * The MCP endpoint is an in-process adapter over `/api`, with a read-only cap (PR 2).
  * A Claude Code plugin marketplace in this repository (PR 3).
* `docs/architecture.md`: the `/mcp` path, `mcp_tokens`, the dispatch.
* README: enabling MCP, minting a token, connecting an agent, installing the plugin.
* `CHANGELOG.md` `Unreleased`: one entry per pull request.
* Roadmap: Cedar caps for MCP, the MCP OAuth flow, further tools; the "Bearer tokens on `/api`"
  item notes that MCP tokens do not open `/api`.
* `pages` pull request, English with `docs-writer` and German with `docs-translator`: enabling MCP,
  minting a token, connecting an agent, the plugin.
