# 2. SQLite by default, an existing Postgres by URL, no bundled Postgres

Date: 2026-11-16 · Status: accepted

## Context

Operators should be able to run Zigrt with no database to manage, or point it at a Postgres they already run. A third option, shipping a Postgres server alongside the app, was considered and declined.

## Decision

The default database is a SQLite file on the `/data` volume. Setting `ZIGRT_DATABASE_URL` to a `postgres://` URL uses an existing Postgres instead. Neither the Compose file nor the Helm chart deploys a Postgres server.

Queries are written once, in a SQL subset both engines accept, with `?` placeholders. A small `Dialect` covers the differences (placeholder style, timestamp encoding, row locking, unique-violation detection). Migrations are the same numbered file in `migrations/sqlite` and `migrations/postgres`, checked by `scripts/check-migrations.sh`. One contract suite, `internal/store/storetest`, runs against both engines.

## Consequences

- A fresh install has nothing to configure and one volume to back up.
- With SQLite the app must run as exactly one replica on block storage, never NFS or SMB.
- Every schema change is written twice and every store behaviour is tested twice.
- Built-in nightly backups cover SQLite only; Postgres operators back up their own database.
