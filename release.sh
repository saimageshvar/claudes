#!/bin/bash
# Cross-builds claudes for macOS and Linux, writes checksums.txt, and publishes a GitHub release (needs gh).
# Usage: ./release.sh v0.1.0
set -euo pipefail
v=${1:?usage: release.sh vX.Y.Z}
cd "$(dirname "$0")"
out=dist
rm -rf "$out"
mkdir -p "$out"
for t in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  os=${t%/*} arch=${t#*/}
  d=$(mktemp -d)
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$v" -o "$d/claudes" .
  tar -C "$d" -czf "$out/claudes_${os}_${arch}.tar.gz" claudes
  rm -rf "$d"
done
(cd "$out" && shasum -a 256 ./*.tar.gz | sed 's| \./| |' > checksums.txt)
gh release create "$v" "$out"/* --title "$v" --generate-notes
