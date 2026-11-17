# 16. The JSON API and MCP server, with parents' bearer tokens

Date: 2026-11-17 · Status: accepted

## Context

Plan section 11 asks for a JSON API and an optional MCP server on Goldberry's pattern, so a parent can script a household (a "print Friday's sheet" job) or ask an AI assistant how the children are doing.

## Decision

- **Where:** `/api/v1` (JSON) and `/mcp` (streamable HTTP, stateless) in the same binary. MCP is off unless `ZIGRT_MCP_ENABLED` is set (Helm: `mcp.enabled`). Both live in `internal/api`, share the DTOs and the operations, and call `service` only.
- **Tokens:** a parent makes one on the Household page. It starts `zg_`, is 256 random bits, stored as a SHA-256 hash, and shown once. It has a name and a last-used time, is revocable, and dies with its parent. Up to 20 live tokens per parent.
- **Scopes:** every token has `read`. `manage` adds changing a student's daily goal, rest days, course and fact-speed checks, and making worksheets. A worksheet needs `manage` because it writes a row, even though it changes no learning.
- **Nothing answers problems or starts lessons.** Learning happens in the web app, so the event log is a child's own work (plan 11).
- **Through MCP, a student is named or given by ID.** "Ava", in any case, works where a name is unique. Tools above a token's scope are neither listed nor callable.
- **Audit:** a change made with a token is audited as its parent, with `via_token` naming the token.
- **Errors** reuse the service's: a refused value is 422 naming the field, an unknown student 404, a missing scope 403 `insufficient_scope`, malformed JSON or an unknown field 400.
- **Throttling:** 10 requests a second per token (bursts of 40). An address that sends 10 bad tokens in a minute is refused for the rest of that minute.
- **The contract:** `internal/api/openapi.json` (OpenAPI 3.1, served at `/api/v1/openapi.json` without a token) is checked against the registered routes and scopes by a test.
- **Not an LLM in Zigrt.** ADR 5 still holds: the MCP server only answers an outside assistant's calls; Zigrt makes no outbound request.

## Consequences

- Home automation and assistants get a stable, narrow contract.
- A leaked read token shows progress, not the power to change it. A leaked manage token can change goals, and the audit log names it.
- Answering through an API stays impossible by design: an "auto-practice" script can't inflate a child's tower.
