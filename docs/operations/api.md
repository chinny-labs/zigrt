# API and MCP

Zigrt has a small JSON API at `/api/v1` and, when turned on, an MCP server at `/mcp` for AI assistants. Both read the students' progress and change a few settings. Neither can answer problems or start lessons: learning happens in the app.

## Tokens

A parent makes a token on **Household → API tokens**:

- Give it a name you'll recognise ("Home Assistant").
- Tick **Also change settings and make worksheets** for the `manage` scope; leave it for read-only.
- Copy the token from the next page; it is shown once. It starts `zg_`.

Send it as a header: `Authorization: Bearer zg_...`. Revoke it on the same page; anything using it stops at once. Each parent sees and revokes only their own tokens, and a parent's tokens go when the parent is deleted.

## The API

The full description is at `/api/v1/openapi.json` (OpenAPI 3.1, no token needed).

| Call | Scope | Gives |
| --- | --- | --- |
| `GET /api/v1/system` | read | Version and content pack |
| `GET /api/v1/students` | read | Each student: today's XP and goal, streak, this week by day, blocks placed, last active, stuck |
| `GET /api/v1/students/{id}` | read | One student, the same |
| `GET /api/v1/students/{id}/topics` | read | Every topic with its state and next review |
| `GET /api/v1/students/{id}/activity` | read | Recent lessons, reviews, quizzes, drills and worksheets |
| `GET /api/v1/students/{id}/struggles?days=30` | read | Topics with two or more lessons stopped or reviews failed |
| `PATCH /api/v1/students/{id}/settings` | manage | Change `daily_xp_goal`, `rest_days`, `course_id` or `fluency` |
| `POST /api/v1/worksheets` | manage | Make a worksheet; returns its URL and problems |

For example:

```sh
curl -H "Authorization: Bearer $ZIGRT_TOKEN" https://zigrt.example.com/api/v1/students
curl -X PATCH -H "Authorization: Bearer $ZIGRT_TOKEN" -d '{"daily_xp_goal": 20}' \
  https://zigrt.example.com/api/v1/students/STUDENT_ID/settings
curl -H "Authorization: Bearer $ZIGRT_TOKEN" -d '{"title": "Friday",
  "settings": {"ops": {"mul": {"on": true, "level": "facts10"}}, "formats": ["across"], "count": 20}}' \
  https://zigrt.example.com/api/v1/worksheets
```

A worksheet's `url` opens for a signed-in parent; the response also has the problems, so a script can lay out its own sheet.

Errors are JSON: `{"error": {"code": "invalid", "message": "...", "field": "daily_xp_goal"}}`. A token makes up to 10 requests a second; past that, or after 10 bad tokens from one address in a minute, the answer is 429 with `Retry-After`.

## MCP

Turn it on with `ZIGRT_MCP_ENABLED=true` (Helm: `mcp.enabled: true`). Then point an assistant that speaks streamable HTTP at `https://zigrt.example.com/mcp` with the token as a bearer header. For Claude Code:

```sh
claude mcp add --transport http zigrt https://zigrt.example.com/mcp --header "Authorization: Bearer zg_..."
```

The tools mirror the API: `get_system`, `list_students`, `get_student`, `list_topics`, `list_activity`, `list_struggles`, and, with `manage`, `update_student_settings` and `create_worksheet`. A student can be named ("Ava") instead of given by ID. A read-only token doesn't see the manage tools at all.

## Behind a proxy

`/api/` and `/mcp` need no cookies, so a reverse proxy passes them through like any other path. Set `ZIGRT_TRUSTED_PROXIES` so the bad-token throttle counts the real caller, not the proxy.
