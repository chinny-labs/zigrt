-- A missed quiz problem brings that topic's review forward (plan 3.6): a new
-- mastery event kind, quiz_missed. SQLite cannot alter a CHECK, so the
-- table is rebuilt with its rows; mastery_events is append-only and nothing
-- references it. Kept in lockstep with migrations/postgres/00004_quiz_missed.sql.

-- +goose Up
CREATE TABLE mastery_events_new (
    id         TEXT PRIMARY KEY,
    student_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    topic_id   TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('lesson_started', 'lesson_paused', 'lesson_passed',
        'review_passed', 'review_failed', 'implicit_credit', 'pulled_forward', 'reopened',
        'placed', 'parent_override', 'quiz_missed')),
    task_id    TEXT,
    detail     TEXT,
    created_at TEXT NOT NULL
);
INSERT INTO mastery_events_new (id, student_id, topic_id, kind, task_id, detail, created_at)
    SELECT id, student_id, topic_id, kind, task_id, detail, created_at FROM mastery_events;
DROP TABLE mastery_events;
ALTER TABLE mastery_events_new RENAME TO mastery_events;
CREATE INDEX mastery_events_student ON mastery_events (student_id, created_at);

-- +goose Down
CREATE TABLE mastery_events_old (
    id         TEXT PRIMARY KEY,
    student_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    topic_id   TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('lesson_started', 'lesson_paused', 'lesson_passed',
        'review_passed', 'review_failed', 'implicit_credit', 'pulled_forward', 'reopened',
        'placed', 'parent_override')),
    task_id    TEXT,
    detail     TEXT,
    created_at TEXT NOT NULL
);
INSERT INTO mastery_events_old (id, student_id, topic_id, kind, task_id, detail, created_at)
    SELECT id, student_id, topic_id, kind, task_id, detail, created_at FROM mastery_events WHERE kind <> 'quiz_missed';
DROP TABLE mastery_events;
ALTER TABLE mastery_events_old RENAME TO mastery_events;
CREATE INDEX mastery_events_student ON mastery_events (student_id, created_at);
