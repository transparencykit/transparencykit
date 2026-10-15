#!/usr/bin/env sh
# Build reproducible static release binaries and a SHA256SUMS file.
#   tools/release.sh v0.1.0
set -eu
version="${1:?usage: tools/release.sh <version>}"
root=$(cd "$(dirname "$0")/.." && pwd)
out="$root/dist/$version"
rm -rf "$out"; mkdir -p "$out"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${target%/*}; arch=${target#*/}
  ext=""; [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/transparencykit/transparencykit/internal/cli.Version=$version" \
    -o "$out/transparencykit-$version-$os-$arch$ext" "$root/cmd/transparencykit"
  # tkit: identical program under its short name
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/transparencykit/transparencykit/internal/cli.Version=$version" \
    -o "$out/tkit-$version-$os-$arch$ext" "$root/cmd/tkit"
done
(cd "$out" && sha256sum transparencykit-* tkit-* > SHA256SUMS)
cat "$out/SHA256SUMS"
