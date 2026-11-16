# 1. One container: a Go binary serving server-rendered HTML

Date: 2026-11-16 · Status: accepted

## Context

Zigrt is run by one household on a home server, often a small one. The person who runs it wants to start it in a minute and upgrade it by changing an image tag. A lesson is a short read followed by one question at a time, which does not need a client-side application.

## Decision

Zigrt is one Go process in one container image. It serves server-rendered HTML with `html/template`; small JavaScript files enhance pages that already work without them. The code is layered `web` → `service` → `store`: handlers parse and render, every rule lives in `service`, and only `store` contains SQL. The image is a static binary on a distroless base, built for amd64 and arm64.

## Consequences

- One thing to deploy, monitor and back up. No message queue, cache or second service.
- All mastery and scheduling rules run on the server, so the browser can never see an answer early or skip a rule.
- No horizontal scaling: one replica, including on Postgres. A household does not need more, and background jobs need no leader election.
- Rich client interactions (the timed drill) are written as small scripts against server-provided data, not as a single-page app.
