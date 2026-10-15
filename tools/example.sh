#!/usr/bin/env sh
# Build the So Sovereign Cloud example: import its records into a fresh ledger,
# publish 2026 and verify the result.
#
#   tools/example.sh <public-dir>     # publish into <public-dir> (e.g. _site/example)
#
# Used by tools/demo.sh and by the website build (.github/workflows/pages.yml).
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
pub=${1:?usage: tools/example.sh <public-dir>}
mkdir -p "$pub"
pub=$(cd "$pub" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
CGO_ENABLED=0 go build -trimpath -o "$work/tkit" "$root/cmd/tkit"
cp "$root/examples/so-sovereign-cloud/transparencykit.json" \
   "$root/examples/so-sovereign-cloud/transparencykit.key" \
   "$root/examples/so-sovereign-cloud/transparencykit.pub" "$work/"
cd "$work"
./tkit record import -f "$root/examples/so-sovereign-cloud/records.json"
./tkit ledger verify
./tkit publish --period 2026 --out "$pub"
./tkit verify --pubkey transparencykit.pub --chain "file://$pub"
