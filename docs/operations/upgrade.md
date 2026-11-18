# Upgrade

Releases follow [semantic versioning](https://semver.org): a patch fixes, a minor version adds, a major version may need you to change something, and its release notes say what. The [releases page](https://github.com/chinny-labs/zigrt/releases) lists every version with its changes.

## How

There is no `latest` tag, so an upgrade is always a version you choose.

- **Helm:** change the chart version (`--version`, or `version:` in a Flux `HelmRelease`). The chart's `appVersion` pins the image to the same release.
- **Docker Compose:** the image tag in `compose.yaml` is `1`, which follows the newest 1.x release on `docker compose pull && docker compose up -d`. Pin a full version (`1.1.0`) to upgrade only when you change it.

## What happens at startup

1. With SQLite, if the new version has database migrations to apply, Zigrt first writes a `premigrate-` snapshot to `/data/backups` ([Backups](backup.md)).
2. It applies the migrations. With Postgres, take your own backup first.
3. It loads the content pack the release embeds, or a newer one from the content folder ([Content packs](content.md)). If the lessons changed, parents get a "new lessons" notification.

Student history carries across: topic IDs are never reused, and a split or merged topic brings its history with it.

## Going back

If the upgrade applied no migrations, run the previous version again. If it did, restore the `premigrate-` snapshot it took and then run the previous version. Taking a backup of your own just before an upgrade (**Back up now** on the Household page, or `zigrt backup`) costs nothing.
