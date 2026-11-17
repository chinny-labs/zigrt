# 12. The embedded content pack is committed with a lock file

Date: 2026-11-17 · Status: accepted

## Context

Plan section 4.7 has each app release embed one pinned content pack, so a fresh install works with no downloads. It said the image build would download the pack. That makes `go build` and `go test` outside Docker run without the pack (or need the network), so the binary a developer runs would differ from the one the image ships.

## Decision

- The pinned pack file itself is committed in `internal/pack`, next to `pack.lock`, which records its calendar version, file name, release URL and SHA-256. `go:embed` puts both in every binary.
- `just pack-update <version>` (`scripts/pack-update.sh`) downloads the release asset and its checksum from `zigrt-curriculum`, checks it, swaps the file and rewrites the lock. Moving to a newer pack is that one command and a pull request.
- At startup the app checks the embedded file against the lock, loads it, and uses a newer pack from `ZIGRT_CONTENT_DIR` if one loads. If the newest pack needs a newer app (`min_app_version`), `/readyz` fails rather than quietly running an older pack.
- Tests load the embedded pack and run every gate on it, plus 90 simulated days, so a release cannot ship a pack its own checks reject.
- Pack files are zip archives whose entries are stored, not compressed: deflate output changed between Go releases, and a pack's checksum must not depend on which Go built it. The plan's `.tar.gz` is a zip.

## Consequences

- Builds are offline and identical everywhere; the image needs no download step.
- The repository carries one pack file at a time, a few tens of kilobytes now and perhaps a megabyte when K-5 is complete. Old ones leave with each update.
- A pack update is reviewed as a pull request like any other change.
