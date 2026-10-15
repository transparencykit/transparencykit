# AGENTS.md — working on vitrine

Context for humans and coding agents resuming work on this repository.

## What this is
A Go CLI/static binary that keeps a hash-chained transparency ledger in S3-compatible storage
(Exoscale SOS) and publishes signed reports: transparency report, EU Data Act Art. 28 page,
DSA Art. 15 CSVs, subprocessor feed. The spec is in `docs/SPEC.md`, which is the source of truth.
If you change behaviour, update the spec in the same change.

## Hard rules
- **No third-party Go modules.** `go.mod` must stay dependency-free (supply-chain argument; also
  the original build environment had no module proxy).
- **The ledger format is frozen once released.** `record.Entry` and canonical JSON define hashes.
  Any change needs `"v": 2` handling and a migration note.
- **Never publish `private`.** Projection types in `internal/report` (`*Out`) are the only things
  rendered. Don't render `record.*` structs directly.
- Breakdown cells go through `Bander`; DSA CSVs never do.
- Keep `CGO_ENABLED=0` builds working (`TestStaticBinary`).

## Code map
| Path | Role |
|---|---|
| `cmd/vitrine` | `main` + end-to-end tests (build the real binary) |
| `internal/cli` | Commands, flag parsing, read-only HTTP `Handler` |
| `internal/canon` | Canonical JSON + SHA-256 helpers |
| `internal/record` | Envelope, body schemas and validation, chain verification, amendment/retraction resolution |
| `internal/ledger` | Append protocol (list → check head → create-only PUT, retry) and verification on a `store.Store` |
| `internal/store` | `Store` interface; `File`; `S3` + hand-written SigV4; URL parsing (`file://`, `sos://`, `s3://`) |
| `internal/s3fake` | In-memory S3 server for tests (pagination, If-None-Match, payload hash checks) |
| `internal/report` | Period parsing, exact aggregation, banded public model, HTML/JSON/Atom/DSA CSV rendering, `Publish` |
| `internal/manifest` | Manifest schema, Ed25519 keys (PKCS#8/PKIX PEM), sign/verify |
| `internal/verify` | Public verification over HTTP or a store, manifest chain walking |
| `internal/config`, `internal/geo` | `vitrine.json`; EU/EEA sets and ISO-country checks |
| `tools/` | Reusable scripts (`demo.sh`, `release.sh`) |
| `examples/records/demo.json` | Fictional 117-record dataset used by the demo |

## Commands
```sh
go test -race ./...                 # all tests (about 50; fake S3, real binary)
tools/demo.sh --serve               # demo publication on :8080
tools/release.sh v0.1.0             # static binaries for linux/darwin amd64/arm64 + SHA256SUMS
```

## Open items / next steps
- Module path `github.com/exoscale/vitrine` is a placeholder; change it if the repository lives elsewhere.
- Validate DSA CSV headers byte-for-byte against the official Annex I template (SPEC §2 caveat).
- Confirm SOS `If-None-Match` behaviour with `TestSOSIntegration`; document the result in the SPEC.
- v0.2: `ledger verify --versions`, signed key rotation, `canary.json`, XLSX export.
- v0.3: anchor manifest hashes in a public transparency log (Sigsum or Tessera + witnesses).
