# 14. Placement, quizzes and review compression

Date: 2026-11-17 · Status: accepted

## Context

Plan sections 3.3, 3.5 and 3.6 describe the placement test, review compression and quizzes in a sentence or two each. Building them needed choices the plan leaves open.

## Decision

- **Placement is the first task for a new student, and skippable.** A student with no history is offered the placement test first; "start from the bottom instead" records it as skipped and it is not offered again (a parent can start one later). Stopping part way keeps it, offered to resume.
- **Choosing the next question.** Over the student's working set (the course and everything it requires), the next topic asked about is the one with the best worst case: a right answer is one step of evidence (of the two needed) for the topic and all it requires; a wrong answer, or "I haven't learned this", settles the topic and all that require it as not known. The topic maximising the smaller of those two counts is asked, ties going to the larger sum, then teaching order. The hardest knowledge point of the topic is asked. Known topics are placed at stage 2 (checked within a week); the rest start locked or ready. At most 35 questions.
- **A missed quiz problem is a mastery event, `quiz_missed`,** that brings the topic's review due today. It is primary (a rebuild replays it), so migration 00004 adds the kind, which in SQLite means rebuilding `mastery_events` with its rows.
- **Quizzes** come after 150 XP since the last quiz, drawing 8 to 10 problems round-robin from the topics whose lessons were passed since then (the newest ten when there are more). XP is 15 scaled by the score, none below half. Nothing is marked until the end.
- **Compression hides a covered review.** A due topic that another due topic fully encompasses (weight 1.0) is left off the list; passing the encompassing review credits it a stage. If that review fails, the covered topic comes back.
- `learning.Config.OfferPlacement` and `OfferQuizzes` switch the features on, so the engine can ship before its screens.

## Consequences

- `zigrt simulate` and the store contract suite play four student profiles for 60 days on the sample pack with both features on: a student who knows grade 2 is placed at exactly the grade 2 topics, review load stays within 10 a day, and no topic is left unreached.
- Downgrading past migration 00004 drops `quiz_missed` events.
