# 10. The authoring tool is built from the app's module

Date: 2026-11-17 · Status: accepted

## Context

Section 4.8 of the plan wants a template to behave the same in authoring, in curriculum CI and in a lesson, so the authoring tool must use exactly the app's `problem` and `content` packages. It also says the curriculum repo ships the tool, and that the two packages form a public, versioned Go module.

## Decision

- `problem`, `problem/expr` and `content` are top-level packages of the app module `github.com/chinny-labs/zigrt`, outside `internal/`, licensed Apache-2.0 (each folder has a `LICENSE`). The app module's release tags version them.
- The tool lives in the app repo as `cmd/zigrt-content`, also Apache-2.0, and is tested there against a prototype pack.
- The curriculum repo pins it with a `tool` line in its `go.mod` and runs `go tool zigrt-content check`. Moving to a newer app release is one `go get`, which also moves the tool to the same packages that release runs.
- The gates are functions (`Template.Gate`, `Pack.Gate`), so the tool, the app's tests and any other program run the same checks.

## Consequences

- There is no second module to tag and keep in step. Go builds only the packages the tool imports, so the curriculum repo never compiles the database drivers.
- The curriculum repo's `go.sum` lists the app's dependencies. That is bookkeeping, not code it builds.
- A change to `problem` or `content` is reviewed as part of the app, with the app's tests.
