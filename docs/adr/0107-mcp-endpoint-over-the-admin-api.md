# 0107. Serve MCP as an in-process adapter over the admin API

* Status: Accepted
* Date: 2026-11-11

## Context

AI assistants speak MCP. Teamster already has an admin API at `/api` whose handlers enforce Cedar
policy, destination grants, validation and audit. An MCP endpoint that reached the store by another
route would have to repeat every one of those checks, and would drift from them.

MCP tokens ([ADR 0106](0106-mcp-tokens-answer-to-their-creator.md)) already identify the creator as
they are now. What is missing is the endpoint that accepts them, and a way to run the tools as that
user without a second authorization path.

## Decision

* **An adapter over `/api`, in process.** `/mcp` is served by the official Go SDK
  (`github.com/modelcontextprotocol/go-sdk`) behind `mcpAuth`. Each tool calls `callAPI`, which
  builds a request for the matching `/api` path and runs it through `authorize(adminMux)` with the
  caller's principal. Cedar, grants, validation and audit are the ones the admin API uses, so a tool
  can do what the user can do in the UI and nothing more. Audit events carry `via = "mcp"`.
* **Stateless.** The SDK handler runs with `Stateless: true`: every request carries the token and
  builds its own server and tool list. There is no session to pin, so no sticky sessions, and a
  revoked token stops working on the next call.
* **Read-only is enforced twice.** Write tools are left out of the tool list when the token is
  `read` or `mcp.read-only` is on. Independently, while the request is read-only (a `read` token, or
  `mcp.read-only` on), `callAPI` refuses anything but a view action or exactly
  `GET /api/samples`, which `authorize` treats as an editor-only read of templates, not a view of
  a record. The second check holds even if a write tool were registered by mistake.
* **A tool id never steers a call elsewhere.** `callAPI` serves `/api/` paths only, with a path
  that is already clean. `apiPath` refuses an id that is empty, contains `/`, or is exactly `.` or
  `..`, and escapes the rest. Every tool must also land on its own mux pattern, so a reserved
  sibling id such as `global-default` cannot turn `/api/routes/{id}` into
  `/api/routes/global-default`.
* **Offered tools follow the user.** `list_sampled_labels` is offered only when samples are on and
  the user may edit templates or routes.
* **A metric.** `teamster.mcp.tool_calls{tool,outcome}` counts every call.

Alternatives considered:

* **A service layer** that both `/api` and MCP call. Cleaner on paper, but it means lifting
  authorization, validation and audit out of the handlers, a large change to code that works,
  for one more caller.
* **A stdio MCP client** that calls `/api` with bearer tokens. It needs `/api` to accept bearer
  tokens, which widens what a leaked token opens, and every user installs and updates a binary.

## Consequences

* A change to an `/api` handler changes the matching tool with it; the tool schemas come from the
  request types.
* **Prompt injection.** Sampled labels and other users' text (names, template bodies) reach an agent
  that may hold write tools, so text an attacker controls can try to steer it. The mitigations are
  `read` tokens, `mcp.read-only`, the server instructions, and the audit trail, which names the
  token's creator and `via = "mcp"`. They reduce the risk; they do not remove it.
* A new dependency: the official SDK and its `jsonschema-go`.
* MCP tokens do not open `/api` itself; only `/mcp` accepts them.
* The SDK's DNS-rebinding guard is off, because it 403s a sidecar proxy that forwards to
  `127.0.0.1` with its own Host header. `/mcp` is bearer-only, so a browser in a rebinding attack
  holds no token.
* No migration: the tokens table came with ADR 0106.
