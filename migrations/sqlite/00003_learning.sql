-- Lessons, reviews and mastery (plan §3, §8): tasks, the append-only logs
-- (attempts, mastery_events, xp_events) and their projections (topic_state,
-- daily_totals), which `zigrt rebuild` recomputes from the logs (ADR 3).
-- Kept in lockstep with migrations/postgres/00003_learning.sql.
-- Local dates (local_date, due_on) are household-local YYYY-MM-DD TEXT.

-- +goose Up
-- A lesson, review, quiz or diagnostic in progress or done. state is JSON the
-- service owns: the knowledge point reached, streaks, the problem on screen.
-- The kinds used in later phases are allowed now: a CHECK cannot be altered
-- in SQLite without rebuilding the table.
CREATE TABLE tasks (
    id           TEXT PRIMARY KEY,
    student_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('lesson', 'review', 'quiz', 'diagnostic')),
    topic_id     TEXT,
    pack_version TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('active', 'paused', 'passed', 'failed', 'abandoned')),
    state        TEXT NOT NULL,
    xp_awarded   INTEGER NOT NULL DEFAULT 0,
    started_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    finished_at  TEXT
);
-- One task is on screen at a time.
CREATE UNIQUE INDEX tasks_one_active ON tasks (student_id) WHERE status = 'active';
CREATE INDEX tasks_student ON tasks (student_id, status, updated_at);

-- Append-only: one row per answered problem. id is the attempt ID issued
-- with the problem, so a repeated submit finds its first answer.
CREATE TABLE attempts (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    student_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    topic_id    TEXT NOT NULL,
    kp_id       TEXT,
    template_id TEXT NOT NULL,
    seed        TEXT NOT NULL,
    answer      TEXT NOT NULL,
    correct     INTEGER NOT NULL,
    note        TEXT,
    response_ms INTEGER NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX attempts_student ON attempts (student_id, created_at);
CREATE INDEX attempts_task ON attempts (task_id, created_at);

-- Append-only: every change to a topic's state and why. detail is JSON.
CREATE TABLE mastery_events (
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
CREATE INDEX mastery_events_student ON mastery_events (student_id, created_at);

-- Projection of mastery_events. A topic with no row is locked or ready,
-- which follows from the graph.
CREATE TABLE topic_state (
    student_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    topic_id    TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('learning', 'mastered')),
    stage       INTEGER NOT NULL DEFAULT 0,
    due_on      TEXT,
    last_result TEXT,
    fail_streak INTEGER NOT NULL DEFAULT 0,
    pauses      INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (student_id, topic_id)
);
CREATE INDEX topic_state_due ON topic_state (student_id, due_on);

-- Append-only: XP earned. A task or a drill pays at most once.
CREATE TABLE xp_events (
    id          TEXT PRIMARY KEY,
    student_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('task', 'drill')),
    source_id   TEXT NOT NULL,
    amount      INTEGER NOT NULL,
    local_date  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE (source_kind, source_id)
);
CREATE INDEX xp_events_student_date ON xp_events (student_id, local_date);

-- Projection of xp_events, with the goal as it was that day.
CREATE TABLE daily_totals (
    student_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    local_date TEXT NOT NULL,
    xp         INTEGER NOT NULL,
    goal       INTEGER NOT NULL,
    goal_met   INTEGER NOT NULL,
    PRIMARY KEY (student_id, local_date)
);

-- +goose Down
DROP TABLE daily_totals;
DROP TABLE xp_events;
DROP TABLE topic_state;
DROP TABLE mastery_events;
DROP TABLE attempts;
DROP TABLE tasks;
