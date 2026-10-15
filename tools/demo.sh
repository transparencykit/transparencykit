#!/usr/bin/env sh
# Build vitrine, create a demo ledger from examples/records/demo.json (fictional
# provider), publish 2026, verify it, and optionally serve it.
#
#   tools/demo.sh            # build + publish into ./demo
#   tools/demo.sh --serve    # ...then serve on http://localhost:8080
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
out="${DEMO_DIR:-$root/demo}"
rm -rf "$out"
mkdir -p "$out"
CGO_ENABLED=0 go build -trimpath -o "$out/vitrine" "$root/cmd/vitrine"
cd "$out"
./vitrine init >/dev/null
./vitrine record import -f "$root/examples/records/demo.json"
./vitrine ledger verify
./vitrine publish --period 2026
./vitrine verify --pubkey vitrine.pub --chain "file://$out/public"
echo "open $out/public/latest/index.html"
if [ "${1:-}" = "--serve" ]; then
  exec ./vitrine serve --addr :8080
fi
