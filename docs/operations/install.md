# Install

Zigrt is one container. It needs one volume, and optionally a Postgres you already run.

Each release publishes a multi-arch image, `ghcr.io/chinny-labs/zigrt:<version>` (also tagged `<major>.<minor>` and `<major>`), and the Helm chart `oci://ghcr.io/chinny-labs/charts/zigrt`. There is no `latest` tag: pin a version. The [releases page](https://github.com/chinny-labs/zigrt/releases) lists them.

## Choose a database

| Mode | How to select it | What runs | Your responsibility |
| --- | --- | --- | --- |
| SQLite (default) | Nothing to set | The app and one volume | Keep the volume on block storage, never NFS or SMB. One replica only |
| Existing Postgres | `ZIGRT_DATABASE_URL=postgres://user:pass@host:5432/zigrt?sslmode=require` | The app only | Create the database and user; back it up |

Zigrt creates and upgrades its own schema at startup in either mode. It does not deploy a Postgres server.

## Docker Compose

```sh
mkdir zigrt && cd zigrt
curl -fsSLO https://raw.githubusercontent.com/chinny-labs/zigrt/main/deploy/compose/compose.yaml
openssl rand -base64 32 > zigrt_key.txt
docker compose up -d
docker compose logs zigrt | grep -i "setup token"
```

Open `/setup`, paste the token and create the first parent. The token is printed once, on the first start, and stops working as soon as a parent exists.

To use Postgres, put the URL in a file, add it as a second secret in `compose.yaml` and uncomment `ZIGRT_DATABASE_URL_FILE`.

## Kubernetes (Helm)

```sh
helm install zigrt oci://ghcr.io/chinny-labs/charts/zigrt --version 0.1.0 \
  --namespace zigrt --create-namespace \
  --set timezone=America/New_York \
  --set baseURL=https://zigrt.home.example.com \
  --set persistence.storageClass=longhorn \
  --set ingress.enabled=true --set 'ingress.hosts[0].host=zigrt.home.example.com'
kubectl logs -n zigrt deploy/zigrt | grep -i "setup token"
```

The chart's version and its `appVersion` are the release, so `--version` pins the image too. Upgrading is changing that version.

The chart runs one replica with strategy `Recreate`, a ReadWriteOnce volume for `/data`, a non-root user and a read-only root filesystem. It refuses to render with more than one replica on SQLite. For Postgres set `postgres.external.url`, or `postgres.external.existingSecret` to read the URL from a Secret you manage. Leave `secretKey` empty to have the chart generate a key into its own Secret and keep it across upgrades (it uses Helm's `lookup`, which Flux's helm-controller runs), or point `secretKey.existingSecret` at a Secret you manage. Argo CD renders charts without cluster access, so `lookup` finds nothing and the key would change on every sync: use `existingSecret` there.

- **Storage class:** set `persistence.storageClass` to a block-backed class (Longhorn, local-path, Ceph RBD). Some clusters, k3s with Longhorn added for example, end up with two default classes, and an omitted class then binds to either. The PVC is kept when the release is uninstalled.
- **TLS in front of the ingress:** if a proxy outside the cluster terminates TLS and forwards plain HTTP to the ingress controller, leave `insecureCookies` off: what matters is that the browser is on HTTPS.
- **Client addresses:** set `trustedProxies` to the CIDR the ingress controller's pods run in (the pod network), so client addresses in the logs come from `X-Forwarded-For`.
- **First run:** the setup token is printed when the server starts with no parent account. A pod restarted before setup prints a new one, so take it from the running pod's log.

With Flux, the chart comes from an OCI `HelmRepository` (the URL is the repository; the chart name goes in the `HelmRelease`):

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata: {name: zigrt, namespace: zigrt}
spec: {type: oci, url: oci://ghcr.io/chinny-labs/charts, interval: 1h}
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata: {name: zigrt, namespace: zigrt}
spec:
  interval: 10m
  chart:
    spec:
      chart: zigrt
      version: "0.1.0"
      sourceRef: {kind: HelmRepository, name: zigrt}
  values:
    timezone: America/New_York
    baseURL: https://zigrt.home.example.com
    persistence: {storageClass: longhorn}
```

## TLS

Session cookies are `Secure`, so sign-in needs HTTPS: put Zigrt behind a reverse proxy or gateway that terminates TLS. For a plain-HTTP trial on a LAN, set `ZIGRT_INSECURE_COOKIES=true` (`insecureCookies: true` in the chart).

## Settings

Every setting is an environment variable. Any of them can be read from a file instead by adding `_FILE` to the name.

| Variable | Default | Purpose |
| --- | --- | --- |
| `ZIGRT_DATABASE_URL` | `sqlite:///data/zigrt.db` | A `sqlite://` path or a `postgres://` URL |
| `ZIGRT_SECRET_KEY` | generated, kept in the database | 32+ random bytes, base64. Signs CSRF tokens |
| `ZIGRT_BASE_URL` | none | The public URL |
| `ZIGRT_LISTEN` | `:8080` | Listen address |
| `TZ` | `UTC` | Time zone offered at setup, and the clock for nightly backups |
| `ZIGRT_BACKUP_SCHEDULE` | `03:15` | Time of the nightly SQLite backup |
| `ZIGRT_BACKUP_RETAIN` | `14` | How many nightly backups to keep |
| `ZIGRT_TRUSTED_PROXIES` | none | CIDRs whose `X-Forwarded-For` is trusted for client addresses in logs and the API's bad-token throttle |
| `ZIGRT_INSECURE_COOKIES` | `false` | Drop the cookie `Secure` flag for plain-HTTP testing |
| `ZIGRT_LOG_FORMAT` | `json` | `json` or `text` |
| `ZIGRT_CONTENT_DIR` | `/data/content` | Folder of content packs newer than the embedded one ([Content packs](content.md)) |
| `ZIGRT_MCP_ENABLED` | `false` | Serve the MCP server at `/mcp` ([API and MCP](api.md)); `mcp.enabled` in the chart |

## Backups

With SQLite, Zigrt writes a snapshot to `/data/backups` each night, keeps the newest 14, and takes another before every upgrade that changes the database. Copy that folder off the machine. With Postgres, back up with your own tools. [Backups, export and restore](backup.md) has the details.

## Commands

Run these inside the container, for example `docker compose exec zigrt zigrt version`.

| Command | Does |
| --- | --- |
| `zigrt serve` | Runs the server (the default) |
| `zigrt migrate` | Applies migrations and exits |
| `zigrt healthcheck` | Exits 0 if the local server is healthy |
| `zigrt reset-password <email>` | Sets a new password for a parent and prints it. `--password-stdin` reads one instead |
| `zigrt backup` | Takes a SQLite backup now |
| `zigrt rebuild` | Recomputes every student's progress from the history |
| `zigrt demo` | Fills an empty database with a demo household, to try Zigrt without a placement |
| `zigrt export [-o file]` and `zigrt import <file>` | Write the household as JSON Lines, or load such a file into an empty database ([Backups](backup.md)) |
| `zigrt version` | Prints the version |

## Health

`/healthz` answers as soon as the process is up. `/readyz` answers once the database is reachable and a content pack is loaded; it fails, saying why, when the content folder holds a pack this version is too old for.
