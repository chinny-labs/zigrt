# Install

Zigrt is one container. It needs one volume, and optionally a Postgres you already run.

> This version has accounts and sign-in only. No image is published until the first release; build one from a checkout with `just image` (or `docker build -t ghcr.io/chinny-labs/zigrt:0 .`).

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
helm install zigrt deploy/helm/zigrt \
  --namespace zigrt --create-namespace \
  --set timezone=America/New_York \
  --set ingress.enabled=true --set 'ingress.hosts[0].host=zigrt.home.example.com'
kubectl logs -n zigrt deploy/zigrt | grep -i "setup token"
```

The chart runs one replica with strategy `Recreate`, a ReadWriteOnce volume for `/data`, a non-root user and a read-only root filesystem. It refuses to render with more than one replica on SQLite. For Postgres set `postgres.external.url`, or `postgres.external.existingSecret` to read the URL from a Secret you manage. Leave `secretKey` empty to have the chart generate a key and keep it across upgrades.

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
| `ZIGRT_TRUSTED_PROXIES` | none | CIDRs whose `X-Forwarded-For` is trusted for client addresses in logs |
| `ZIGRT_INSECURE_COOKIES` | `false` | Drop the cookie `Secure` flag for plain-HTTP testing |
| `ZIGRT_LOG_FORMAT` | `json` | `json` or `text` |

## Backups

With SQLite, Zigrt writes a snapshot to `/data/backups` each night and keeps the newest 14. Copy that folder off the machine: a backup on the same disk is not a backup. `zigrt backup` takes one now. To restore, stop the container and copy a snapshot over `/data/zigrt.db`.

With Postgres, back up the database with your own tools.

## Commands

Run these inside the container, for example `docker compose exec zigrt zigrt version`.

| Command | Does |
| --- | --- |
| `zigrt serve` | Runs the server (the default) |
| `zigrt migrate` | Applies migrations and exits |
| `zigrt healthcheck` | Exits 0 if the local server is healthy |
| `zigrt reset-password <email>` | Sets a new password for a parent and prints it. `--password-stdin` reads one instead |
| `zigrt backup` | Takes a SQLite backup now |
| `zigrt version` | Prints the version |

## Health

`/healthz` answers as soon as the process is up. `/readyz` answers once the database is reachable.
