-- Drills and worksheets a student or parent built, and how each on-screen
-- drill went (plan §5). A drill's problems are kept as written, so an old
-- worksheet reprints the same even if the generator changes.
-- Kept in lockstep with migrations/sqlite/00002_drills.sql.

-- +goose Up
CREATE TABLE drills (
    id         TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('drill', 'worksheet')),
    title      TEXT NOT NULL,
    settings   TEXT NOT NULL,
    seed       TEXT NOT NULL,
    problems   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX drills_owner_created ON drills (owner_id, created_at);

-- One row per finished on-screen drill; a drill is finished at most once.
CREATE TABLE drill_results (
    drill_id    TEXT PRIMARY KEY REFERENCES drills (id) ON DELETE CASCADE,
    student_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    correct     INTEGER NOT NULL,
    total       INTEGER NOT NULL,
    duration_ms BIGINT  NOT NULL,
    answers     TEXT NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX drill_results_student ON drill_results (student_id, finished_at);

-- +goose Down
DROP TABLE drill_results;
DROP TABLE drills;
