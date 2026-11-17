# 13. Lesson preview is a command of the app, not of the authoring tool

Date: 2026-11-17 · Status: accepted

## Context

Plan section 4.8 lists `zigrt-content preview <topic>`, which renders a lesson "using the app's own layouts and CSS". The authoring tool is Apache-2.0 and imports only the Apache-2.0 `problem`, `content` and `figure` packages (ADR 10); the layouts, templates and CSS are in `internal/web`, which is AGPL-3.0-only. Either the tool links AGPL code, or the UI is relicensed, or preview lives in the app.

## Decision

Preview is `zigrt preview [-pack dir] [-out dir] [-seeds n] [topic...]`, a command of the app binary. It renders each topic's explanations, with the worked example in full, and problems from every template before and after answering, as static pages with the app's CSS, through the same `taskData` and templates the running app uses. It needs no database or configuration.

The curriculum repository pins it with a second `tool` line, `github.com/chinny-labs/zigrt/cmd/zigrt`, next to `zigrt-content`, and runs `go tool zigrt preview`.

## Consequences

- What an author previews is exactly what a child sees, with no second copy of the layouts to keep in step.
- `zigrt-content` stays Apache-2.0 and light. Previewing compiles the whole app, including the pure-Go SQLite driver, once per cache.
- Running the AGPL app locally to preview places no obligation on authors; the curriculum stays CC BY-SA 4.0.
