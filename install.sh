#!/bin/bash
# claudes installer (private repo): needs an authenticated `gh` and Go.
set -euo pipefail
for c in gh go; do command -v "$c" >/dev/null || { echo "$c is required" >&2; exit 1; }; done
dest=${1:-$HOME/.local/bin}
mkdir -p "$dest"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
gh repo clone saimageshvar/claudes "$tmp" -- --depth 1 -q
(cd "$tmp" && go build -o "$dest/claudes" .)
echo "installed $dest/claudes"
case ":$PATH:" in *":$dest:"*) ;; *) echo "note: add $dest to your PATH" ;; esac
