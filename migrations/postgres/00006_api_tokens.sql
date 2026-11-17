-- API tokens for the JSON API and MCP (plan §11): minted by a parent,
-- stored as a hash, shown once. Kept in lockstep with
-- migrations/sqlite/00006_api_tokens.sql.

-- +goose Up
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label        TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    scopes       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX api_tokens_user ON api_tokens (user_id, created_at);

-- +goose Down
DROP TABLE api_tokens;
