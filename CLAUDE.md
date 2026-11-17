# Zigrt

Self-hosted mastery-learning maths for one household. Go + server-rendered HTML, one container, SQLite (default) or Postgres.

- **Source of truth:** `docs/design/plan.md` (snapshot of the living plan doc) and `docs/design/` (board sources, PNG renders, tokens). Decisions go in `docs/adr/`.
- **Layers:** `web` → `service` → `store`. Handlers parse input, call one service method and render; they never call the store for writes. Every rule lives in `service`. Only `store` has SQL.
- **Pure packages:** algorithms that need no database or HTTP (`problem`, `content`, `drill`, `learning`) take a clock, a PRNG or a seed as arguments and never read the time or random numbers themselves. Every mastery number is a field of `learning.Config`; the service calls `Config.Apply` for every mastery event, and a rebuild folds the log through the same function (ADR 11).
- **Simulated students:** `internal/sim` plays scripted students through the real service with a fake clock; the store contract suite runs 60 simulated days on both engines and checks every review against the schedule. Add a case there when a rule changes.
- **Content:** `problem`, `problem/expr`, `figure`, `content` and `cmd/zigrt-content` are Apache-2.0 and used by the curriculum repo (ADR 10). Template format: `docs/content/templates.md`; its examples are tested. New expression functions get a test and never change meaning once released. The 20-topic prototype pack in `content/testdata/hardest` must keep passing every gate. The app embeds a released curriculum pack, pinned in `internal/pack/pack.lock`; change it only with `just pack-update <version>` (ADR 12). Authors preview lessons with `zigrt preview` (ADR 13).
- **History is append-only:** `attempts`, `mastery_events` and `xp_events` are never updated or deleted; a correction is a new event. Projections must be rebuildable from the logs.
- **Queries are written once:** a portable SQL subset with `?` placeholders in `internal/store/sql.go`; the `Dialect` covers what differs between engines.
- **Migrations:** add the same numbered file to both `migrations/sqlite` and `migrations/postgres`; `scripts/check-migrations.sh` enforces it.
- **Tests:** any new store or service behaviour gets a case in `internal/store/storetest` so it runs on both engines. `just test`, `just test-pg`, `just lint`, `just e2e`.
- **UI:** follow the boards in `docs/design/board` and the tokens in `internal/web/static/app.css`. Figures are SVG drawn by `figure` with classes only (`fig-*`), coloured by `app.css`. A lesson page never contains the answer before it is submitted: the problem is drawn from `TaskView.Problem` without its `Answer`. No inline `style=`, no inline scripts, no CDN assets (the CSP blocks them, and a test checks the templates). Pages must work without JS; the on-screen drill is the one planned exception.
- **Words:** student screens say "blocks", "placed" and "mend"; parent screens say "topics", "mastered" and "review". Nothing is red or green: blue is done or do-this, sun is in progress or needs attention.
- **Releases:** conventional commits; merging the release-please PR tags and publishes (`.github/workflows/release.yml`). The Helm chart version in `deploy/helm/zigrt/Chart.yaml` is bumped by release-please. Operations docs live in `docs/operations/`.
- **Docs change in the same PR as the code.**
