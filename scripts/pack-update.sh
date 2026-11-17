#!/usr/bin/env sh
# Pins a curriculum release as the embedded content pack (ADR 12):
# downloads zigrt-k5-<version>.zip and its .sha256 from the zigrt-curriculum
# release, checks the sum, swaps the zip in internal/pack and rewrites
# pack.lock. Usage: scripts/pack-update.sh 2026.12.1
set -eu
version="${1:?usage: scripts/pack-update.sh <calendar version, e.g. 2026.12.1>}"
echo "$version" | grep -Eq '^[0-9]{4}\.[0-9]{2}\.[0-9]+$' || { echo "not a calendar version: $version" >&2; exit 2; }
repo="${ZIGRT_CURRICULUM_REPO:-chinny-labs/zigrt-curriculum}"
file="zigrt-k5-${version}.zip"
url="https://github.com/${repo}/releases/download/${version}/${file}"
cd "$(dirname "$0")/../internal/pack"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$file" "$url"
curl -fsSL -o "$tmp/$file.sha256" "$url.sha256"
(cd "$tmp" && sha256sum -c "$file.sha256" >/dev/null) || { echo "checksum mismatch for $file" >&2; exit 1; }
sum=$(cut -d' ' -f1 "$tmp/$file.sha256")
rm -f zigrt-k5-*.zip
mv "$tmp/$file" .
cat > pack.lock <<LOCK
# The content pack this build embeds (ADR 12). Rewrite it with
# \`just pack-update <version>\`, which downloads the release asset and checks it.
version $version
file $file
url $url
sha256 $sum
LOCK
echo "Embedded pack is now $file ($sum). Run just test: the pack must pass every gate and load."
