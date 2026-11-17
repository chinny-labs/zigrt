# Backups, export and restore

## SQLite (the default)

Zigrt backs up its own SQLite database. Every snapshot is a consistent copy made with `VACUUM INTO`, written to `/data/backups` next to the database.

| Snapshot | When | Named | Kept |
| --- | --- | --- | --- |
| Nightly | Each day at `ZIGRT_BACKUP_SCHEDULE` (03:15) | `zigrt-YYYYMMDD.db` | The newest `ZIGRT_BACKUP_RETAIN` (14) |
| On demand | **Back up now** on the Household page, or `zigrt backup` | `zigrt-YYYYMMDD-HHMMSS.db` | Pruned with the nightly ones |
| Before an upgrade | When a new version has database migrations to apply, before it applies them | `premigrate-vN-YYYYMMDD-HHMMSS.db` (N is the schema version it came from) | Not pruned; delete them yourself |

A backup on the same disk is not a backup: copy `/data/backups` somewhere else, for example with a nightly `kubectl cp` or your volume's own snapshots.

### Restoring

Stop Zigrt, copy a snapshot over `/data/zigrt.db`, remove `zigrt.db-wal` and `zigrt.db-shm` if they are there, and start it again.

```sh
# Kubernetes: scale down, restore from a helper pod that mounts the PVC, scale up.
kubectl scale -n zigrt deploy/zigrt --replicas=0
# ... copy backups/zigrt-20261117.db over zigrt.db on the volume ...
kubectl scale -n zigrt deploy/zigrt --replicas=1
```

To undo an upgrade, restore the `premigrate-` snapshot it took and run the previous image version.

## Postgres

Back up the database with your own tools (`pg_dump`, your operator's backups). Zigrt's built-in snapshots are for SQLite only.

## Export and import

An export is the whole household as JSON Lines: every student, every answer, all progress and settings, in a form that is the same from SQLite and Postgres. Sessions are left out, so after an import everyone signs in again.

- **Export everything** on the Household page downloads it, or run `zigrt export -o household.jsonl` in the container.
- `zigrt import household.jsonl` loads it into an **empty** database (it refuses one that already has a household).

To move from SQLite to Postgres (or back): export, start Zigrt against the new database once so it creates its tables, stop it, run `zigrt import` with `ZIGRT_DATABASE_URL` pointing at the new database, and start it again.
