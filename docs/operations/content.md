# Content packs

The lessons come from a content pack: the topic graph, the lessons and their problem templates, built and released by [zigrt-curriculum](https://github.com/chinny-labs/zigrt-curriculum). How to write one is in [Writing content](../content/templates.md).

## The pack that ships

Every Zigrt release embeds one pack, pinned by version and checksum in `internal/pack/pack.lock`, so a new install has lessons with no download. The log says which pack is running:

```sh
kubectl logs -n zigrt deploy/zigrt | grep "content loaded"
docker compose logs zigrt | grep "content loaded"
```

## Installing a newer pack

Packs release on a calendar version (`2026.12.1`) more often than the app. To run a newer one without waiting for an app release, put the pack file in the content folder, `ZIGRT_CONTENT_DIR` (`/data/content` by default, on the data volume), and restart:

```sh
# Docker Compose
docker compose cp zigrt-k5-2026.12.1.zip zigrt:/data/content/
docker compose restart zigrt

# Kubernetes: copy into the pod, then delete the pod so it restarts
kubectl cp zigrt-k5-2026.12.1.zip zigrt/<pod>:/data/content/
kubectl delete pod -n zigrt <pod>
```

At start the app loads the embedded pack and every pack file (or unpacked pack folder) in the content folder, and runs the newest. A file that does not load is skipped with a warning in the log. A pack for another curriculum (a different `id`) is skipped too.

Each pack declares the oldest app it needs (`min_app_version`). If the newest pack in the folder needs a newer Zigrt, the app stays **not ready** (`/readyz` answers 503 and says why) instead of running an older pack than the one you installed: upgrade the app, or remove the file.

Student history is unaffected by a pack change: topic and template IDs are never renamed or reused, and a split or merged topic carries its history through the pack's transitions.

## Commands

| Command | Does |
| --- | --- |
| `zigrt rebuild` | Recomputes every student's progress (topic states, daily XP) from the history. Safe to run any time; needed only after a rule change or by hand-repair |
| `zigrt demo [-days 28]` | Fills an **empty** database with a demo household: a parent and two students with weeks of history, played through the real rules. Prints how to sign in. Refuses a database that has a household |
| `zigrt preview -pack <dir> [-out preview] [-seeds 3] [topic...]` | For curriculum authors: renders lessons to static pages with the app's own look, to open from disk |
