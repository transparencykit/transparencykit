#!/usr/bin/env sh
# Publish the So Sovereign Cloud example (a fictional provider, 117 records)
# into ./demo/public and optionally serve it.
#
#   tools/demo.sh            # build + publish into ./demo/public
#   tools/demo.sh --serve    # ...then serve on http://localhost:8080
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
out="${DEMO_DIR:-$root/demo}"
rm -rf "$out"
mkdir -p "$out"
"$root/tools/example.sh" "$out/public"
echo "open $out/public/latest/index.html"
if [ "${1:-}" = "--serve" ]; then
  CGO_ENABLED=0 go build -trimpath -o "$out/tkit" "$root/cmd/tkit"
  exec "$out/tkit" serve --addr :8080 --store "file://$out/public"
fi
