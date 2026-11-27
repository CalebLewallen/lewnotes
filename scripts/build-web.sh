#!/usr/bin/env bash
# Build the SvelteKit SPA and refresh the assets embedded by lewnotes-core.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest="$root/server/internal/webui/dist"

cd "$root/web"
npm run build

rm -rf "$dest"
mkdir -p "$dest"
cp -R build/. "$dest"/

echo "web build copied to server/internal/webui/dist"
