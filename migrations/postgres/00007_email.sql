-- Email (plan §10, §7.5): an outbox the sender job drains with retries, and
-- one-hour password reset links. Kept in lockstep with
-- migrations/sqlite/00007_email.sql.

-- +goose Up
CREATE TABLE email_outbox (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    to_addr         TEXT NOT NULL,
    subject         TEXT NOT NULL,
    body            TEXT NOT NULL,
    dedupe_key      TEXT NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    sent_at         TIMESTAMPTZ,
    last_error      TEXT
);
CREATE INDEX email_outbox_due ON email_outbox (sent_at, next_attempt_at);

CREATE TABLE password_resets (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX password_resets_user ON password_resets (user_id, created_at);

-- +goose Down
DROP TABLE password_resets;
DROP TABLE email_outbox;
