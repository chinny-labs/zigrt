# 11. Mastery history records what happened; a rebuild works out the rest

Date: 2026-11-17 · Status: accepted

## Context

ADR 3 makes `mastery_events` the source of truth for each topic's state, with `topic_state` a projection that `zigrt rebuild` recomputes. Writing the rules (plan section 3) raised three questions the plan left open: which events a rebuild replays, how a rebuild learns that a lesson is in progress or paused, and what "due tomorrow" means in a household that spans midnight in one time zone.

## Decision

- **Primary and derived events.** An event is either a fact about what a student or parent did (`lesson_started`, `lesson_paused`, `lesson_passed`, `review_passed`, `review_failed`, `placed`, `parent_override`) or a consequence the rules derived from one (`implicit_credit`, `pulled_forward`, `reopened`). Both are written, so the log explains every date that moved. A rebuild replays only primary events through the same `learning.Config.Apply` the live path uses, and works the derived ones out again. A change to the rules therefore applies to old history on the next rebuild.
- **Two event kinds beyond the plan.** `lesson_started` and `lesson_paused` exist so that "learning" and the pause count (two pauses bring the prerequisites' reviews forward) can be rebuilt from the log alone. `pulled_forward` records both that and failure flowing up to encompassing topics.
- **Reviews fall due on a day.** `topic_state.due_on` and `xp_events.local_date` are household-local calendar dates (`YYYY-MM-DD`), not instants. Each event's detail records its day, so a rebuild does not depend on the household's current time zone.
- **Locked and Ready are not stored.** Only `learning` and `mastered` have rows; the rest follows from the graph.
- **Defaults chosen where the plan gave a range.** A review is 5 problems and passes at 4 (80%); it is worth 4 XP, +1 when perfect. Implicit credit follows direct `encompasses` edges only, and never moves a topic further out than a passed review of it would. A lesson paused by three misses is not offered again the same day. A student with no course works on the pack's first course and everything it requires. All of these are fields of `learning.Config`.

## Consequences

- `zigrt rebuild` and `Service.CheckProjections` agree with the live path by construction; the store contract suite checks it after every simulated day on both engines.
- The event log is larger than the primary facts alone, which at household scale does not matter.
- A rule change can move due dates on rebuild. That is the point, but it should be noted in release notes when it happens.

## Amendment, 2026-11-18: held lessons' reviews jump the queue

A lesson stopped twice waits until its prerequisites are no longer due. With a 147-topic pack, simulated students carried a backlog of overdue reviews: about half their mastered topics, because the daily goal ends a session after three or four tasks. Reviews are offered most overdue first, so the held lesson's prerequisites could sit behind dozens of older ones and the lesson never came back. A "struggling" student (learns 70%, forgets 25%) stalled for months on one early topic in 4 of 20 runs, mastering 94 to 127 of 147 topics in 200 days.

Now the due prerequisites of any held lesson come first, ordered by the held lesson's place in the course, and are never left out by compression. That covers a lesson with a task in progress, set aside or reopened. Once they are reviewed, the lesson resumes at the front. The same 20 runs master 146 or 147 of 147; the steady, forgetful and grade 2 profiles are unchanged. The backlog itself is a separate tuning question.
