# 6. The secret key is optional; a generated key is the fallback

Date: 2026-11-16 · Status: accepted

## Context

The plan lists `ZIGRT_SECRET_KEY` as the only required setting. Its one use today is signing CSRF tokens. Sessions are random tokens whose hashes are stored in the database, so they need no signing key.

## Decision

When `ZIGRT_SECRET_KEY` is set, it signs CSRF tokens. When it is not, Zigrt generates a random key on first start and keeps it in the `settings` table. The Compose file and the Helm chart still supply a key.

## Consequences

- `docker run` with no configuration works for a trial.
- A database backup taken without an external key contains the CSRF key. That key protects forms, not data, and rotating it only expires open forms.
- If a later feature needs to encrypt stored secrets (SMTP passwords), the key will have to become required for that feature. This differs from the plan and is recorded here for that reason.
