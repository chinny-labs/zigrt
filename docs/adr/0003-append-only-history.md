# 3. Learning history is append-only; mastery state is a projection

Date: 2026-11-16 · Status: accepted (applies from the first learning tables)

## Context

What a child has mastered and when each topic is next due are computed from what they did. The rules that compute them will be tuned, and the curriculum's topics will be split and merged over time.

## Decision

Every answered problem, every change to a topic's state and every XP award is an immutable row (`attempts`, `mastery_events`, `xp_events`). The tables the app reads for speed (`topic_state`, `daily_totals`) are projections written in the same transaction, and a `zigrt rebuild` command recomputes them from the logs. A correction is a new event, never an edit. Deleting a student removes all of their rows; that is the one exception, for privacy.

## Consequences

- A change to the rules can be applied to existing history by rebuilding.
- Tests can prove the projections and the logs agree after every scenario.
- Topic and template IDs in the logs are opaque strings that are never renamed or reused; the content pack carries transitions for splits and merges.
- The logs grow without bound, which at household scale is not a concern.
