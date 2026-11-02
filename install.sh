#!/bin/bash
# claudes installer: downloads the release binary for this OS and CPU, verifies its checksum, and installs it.
#   curl -fsSL https://raw.githubusercontent.com/saimageshvar/claudes/master/install.sh | bash
# Install into another directory:  ... | bash -s -- /some/dir     (default: $CLAUDES_INSTALL_DIR or ~/.local/bin)
# Pin a version:                   ... | CLAUDES_VERSION=v0.1.0 bash
set -euo pipefail

repo=saimageshvar/claudes
die() { echo "claudes install: $*" >&2; exit 1; }
# GitHub's release CDN is occasionally slow or stalls; bound every download and retry it.
get() { curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 90 "$@"; }

dest=${1:-${CLAUDES_INSTALL_DIR:-$HOME/.local/bin}}
case $(uname -s) in Darwin) os=darwin ;; Linux) os=linux ;; *) die "unsupported OS $(uname -s) (macOS and Linux only)" ;; esac
case $(uname -m) in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) die "unsupported CPU $(uname -m)" ;; esac
command -v curl >/dev/null || die "curl is required"
command -v tar >/dev/null || die "tar is required"

asset=claudes_${os}_${arch}.tar.gz
if [ -n "${CLAUDES_VERSION:-}" ]; then base=https://github.com/$repo/releases/download/$CLAUDES_VERSION
else base=https://github.com/$repo/releases/latest/download; fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$dest"
dest=$(cd "$dest" && pwd)

if get "$base/$asset" -o "$tmp/$asset" && get "$base/checksums.txt" -o "$tmp/checksums.txt"; then
  want=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")
  got=$( (sha256sum "$tmp/$asset" 2>/dev/null || shasum -a 256 "$tmp/$asset") | awk '{ print $1 }')
  [ -n "$want" ] && [ "$want" = "$got" ] || die "checksum mismatch for $asset; not installing"
  tar -xzf "$tmp/$asset" -C "$tmp" claudes
  install -m 0755 "$tmp/claudes" "$dest/claudes"
else
  # No release asset for this platform or version: build from source when the toolchain is here.
  command -v git >/dev/null && command -v go >/dev/null || die "no release found for $asset and git + Go are not installed to build from source"
  echo "no release asset found; building from source" >&2
  git clone -q --depth 1 ${CLAUDES_VERSION:+--branch "$CLAUDES_VERSION"} "https://github.com/$repo.git" "$tmp/src"
  (cd "$tmp/src" && go build -trimpath -o "$dest/claudes" .)
fi

echo "installed $("$dest/claudes" --version) -> $dest/claudes"
case ":$PATH:" in *":$dest:"*) ;; *) echo "note: add $dest to your PATH, e.g.  echo 'export PATH=\"$dest:\$PATH\"' >> ~/.zshrc" ;; esac
command -v claude >/dev/null || echo "note: the claude CLI is not on your PATH; claudes needs it to resume sessions"
