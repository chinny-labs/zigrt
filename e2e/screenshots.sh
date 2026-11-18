#!/usr/bin/env bash
# Build Zigrt, fill a throwaway database with the demo household, serve it,
# and retake docs/screenshots from it (and a few lesson pages from the
# embedded pack's preview).
#   e2e/screenshots.sh
set -euo pipefail
cd "$(dirname "$0")/.."
# The footer shows the version, so stamp the latest release's.
version=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
go build -ldflags "-X github.com/chinny-labs/zigrt/internal/buildinfo.Version=${version:-dev}" -o bin/zigrt ./cmd/zigrt
tmp=$(mktemp -d)
pack=$(ls internal/pack/*.zip | head -1)
export ZIGRT_DATABASE_URL="sqlite://$tmp/zigrt.db" ZIGRT_INSECURE_COOKIES=true ZIGRT_LOG_FORMAT=text \
  TZ=UTC ZIGRT_LISTEN=127.0.0.1:18082
out=$(bin/zigrt demo -days 60)
password=$(sed -n 's/.*Parent: .*, password \(.*\)$/\1/p' <<<"$out")
[ -n "$password" ] || { echo "no demo password in: $out" >&2; exit 1; }
bin/zigrt preview -pack "$pack" -out "$tmp/preview" -seeds 1 \
  make-10 dollars-and-cents same-size-fractions mult-area-model measure-angles >/dev/null
bin/zigrt >"$tmp/server.log" 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; rm -rf "$tmp"' EXIT
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null http://127.0.0.1:18082/readyz 2>/dev/null; then break; fi
  sleep 1
done
(cd e2e && BASE_URL=http://127.0.0.1:18082 PASSWORD="$password" PREVIEW="$tmp/preview" OUT=../docs/screenshots node screenshots.mjs)
