-- The parent side (plan §6.3, §8): notifications, drills a parent assigns,
-- the jobs' bookkeeping and uploaded content packs.
-- Kept in lockstep with migrations/sqlite/00005_household.sql.

-- +goose Up
-- One row per parent. dedupe_key stops the same news arriving twice (a goal
-- met is news once a day). payload is JSON.
CREATE TABLE notifications (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    subject_id TEXT,
    payload    TEXT,
    dedupe_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    read_at    TIMESTAMPTZ,
    UNIQUE (user_id, dedupe_key)
);
CREATE INDEX notifications_user ON notifications (user_id, created_at);

-- A drill or worksheet a parent set for one student (plan 5.1). It shows on
-- the student's home until done; drill_id is the set made when they start.
CREATE TABLE drill_assignments (
    id         TEXT PRIMARY KEY,
    student_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    parent_id  TEXT,
    kind       TEXT NOT NULL CHECK (kind IN ('drill', 'worksheet')),
    title      TEXT NOT NULL,
    settings   TEXT NOT NULL,
    drill_id   TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    done_at    TIMESTAMPTZ
);
CREATE INDEX drill_assignments_student ON drill_assignments (student_id, created_at);

-- The last household-local day each job ran for, so a job catches up after
-- downtime and never runs twice for one day (plan 7.5).
CREATE TABLE job_runs (
    job          TEXT PRIMARY KEY,
    last_run_for TEXT NOT NULL
);

-- Content packs a parent uploaded (plan 4.7). The files live in the content
-- folder; this records who installed which.
CREATE TABLE content_packs (
    version      TEXT PRIMARY KEY,
    sha256       TEXT NOT NULL,
    file         TEXT NOT NULL,
    installed_by TEXT,
    installed_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE content_packs;
DROP TABLE job_runs;
DROP TABLE drill_assignments;
DROP TABLE notifications;
