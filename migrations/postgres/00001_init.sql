-- Zigrt schema: the household, its people and their sign-in state.
-- Kept in lockstep with migrations/sqlite/00001_init.sql (plan §8.3).
-- IDs are UUIDv7 TEXT; times are TIMESTAMPTZ.

-- +goose Up
CREATE TABLE household (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    timezone   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- Parents have an email and a password; students have a PIN and no email.
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    role          TEXT NOT NULL CHECK (role IN ('parent', 'student')),
    display_name  TEXT NOT NULL,
    avatar        TEXT,
    email         TEXT UNIQUE,
    password_hash TEXT,
    pin_hash      TEXT,
    created_at    TIMESTAMPTZ NOT NULL
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_label TEXT,
    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE auth_attempts (
    user_id         TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    failures        INTEGER NOT NULL DEFAULT 0,
    window_start    TIMESTAMPTZ,
    next_allowed_at TIMESTAMPTZ,
    locked_at       TIMESTAMPTZ
);

-- rest_days is a string of weekday numbers, 0 = Sunday ("06" = weekends).
CREATE TABLE student_settings (
    user_id         TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    course_id       TEXT,
    daily_xp_goal   INTEGER NOT NULL DEFAULT 15 CHECK (daily_xp_goal BETWEEN 1 AND 120),
    rest_days       TEXT NOT NULL DEFAULT '',
    fluency_enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE settings (
    key    TEXT PRIMARY KEY,
    value  TEXT NOT NULL,
    secret BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE audit_log (
    id         TEXT PRIMARY KEY,
    actor_id   TEXT,
    action     TEXT NOT NULL,
    target_id  TEXT,
    detail     TEXT,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX audit_created ON audit_log (created_at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE settings;
DROP TABLE student_settings;
DROP TABLE auth_attempts;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE household;
