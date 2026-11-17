# 8. A drill stores its problems, not just its seed

Date: 2026-11-17 · Status: accepted

## Context

The plan said a worksheet's settings and seed are enough to regenerate it, since generation is deterministic. That holds only while the generator never changes. A fix to the generator (a better rule for avoiding repeats, a new level) would silently change every old worksheet and every saved drill result.

## Decision

A saved drill keeps its settings, its seed and the problems themselves, as JSON. The seed is still recorded, and generation is still deterministic, so tests can rely on it. "New version" makes a new drill with a new seed. A finished drill's answers are kept with whether each was right, as marked by the server.

## Consequences

- An old worksheet reprints exactly, and an old result can always be shown against the problems that were actually asked.
- The generator can change freely.
- Each drill row holds up to 100 small problems, a few kilobytes at most.
- Drills are not part of the append-only learning history (ADR 3): they are free practice and are deleted with their owner.
