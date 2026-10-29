#!/bin/bash
# claudes installer: needs git and Go; builds from source.
set -euo pipefail
for c in git go; do command -v "$c" >/dev/null || { echo "$c is required" >&2; exit 1; }; done
dest=${1:-$HOME/.local/bin}
mkdir -p "$dest"
dest=$(cd "$dest" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git clone -q --depth 1 https://github.com/saimageshvar/claudes.git "$tmp"
(cd "$tmp" && go build -o "$dest/claudes" .)
echo "installed $dest/claudes"
case ":$PATH:" in *":$dest:"*) ;; *) echo "note: add $dest to your PATH" ;; esac
