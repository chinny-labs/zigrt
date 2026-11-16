# 4. The curriculum is a separate, versioned content pack

Date: 2026-11-16 · Status: accepted

## Context

The curriculum (topics, the prerequisite graph, lessons, problem templates) is far larger than the app, changes on its own schedule, and should be replaceable by an operator.

## Decision

The curriculum lives in its own repository, `zigrt-curriculum`, and is released as one pack file with a calendar version and a `minAppVersion`. Each app release embeds one pinned pack so a fresh install works with no downloads. A newer pack can be mounted or uploaded. Packs contain data only: problem templates are declarative and evaluated in a sandbox, and the app never runs code from a pack. The app is AGPL-3.0-only, the curriculum CC BY-SA 4.0, and the Go packages the curriculum's authoring tool imports are Apache-2.0.

## Consequences

- The app knows nothing about any particular topic.
- A content fix does not need an app release, and an app release does not wait for content.
- The pack format is a compatibility contract between two repositories and needs its own validation and versioning.
