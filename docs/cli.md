# CLI reference

The binary is `transparencykit`; `tkit` is the same program under a short name. Every command
reads `./transparencykit.json` unless you pass `--config path` or set `TRANSPARENCYKIT_CONFIG`.

| Exit code | Meaning |
|---|---|
| `0` | Success |
| `1` | Verification failure (`ledger verify`, `verify`) |
| `2` | Usage, configuration or I/O error |

## Setup

### `tkit init [--dir .]`

Writes `transparencykit.json`, `transparencykit.key` (mode 0600) and `transparencykit.pub` into
the directory. Refuses to overwrite existing files. Prints the bucket setup commands for Exoscale
SOS.

### `tkit keygen --out transparencykit.key`

Generates an Ed25519 key pair in standard PEM formats (PKCS#8 private, PKIX public), readable by
OpenSSL. The public key is written next to the private key with a `.pub` extension.

## Recording

### `tkit record template <type>`

Prints a filled-in example body for `gov_request`, `abuse_notice`, `subprocessor_change`,
`infrastructure`, `measures` or `retraction`. Redirect it to a file and edit.

### `tkit record add <type> [-f file] [--amends N] [--by who]`

Validates the body and appends it to the ledger. Reads standard input when `-f` is `-` or
omitted.

| Flag | Meaning |
|---|---|
| `-f file` | JSON body to append |
| `--amends N` | This record supersedes record N (same type, original record only) |
| `--by who` | Free-text operator identity stored as `recorded_by` |

Validation rejects unknown fields, non-ISO country codes (`UK` must be `GB`), malformed dates and
timestamps, and any value outside the allowed lists. Nothing is written if validation fails.

### `tkit record import -f records.json`

Appends a JSON array of `{"type", "body", "amends"?, "by"?}` objects. Every item is validated
before the first one is written.

### `tkit record retract <seq> --reason "…"`

Appends a retraction. The retracted record and its amendments are excluded from publications;
the original stays in the ledger.

### `tkit record list [--type t] [--json]` and `tkit record show <seq>`

Operator views of the ledger. They include `private` fields, so do not share their output.

## Ledger

### `tkit ledger verify [--expect-head sha256:…]`

Recomputes every record hash, checks sequence numbers, `prev` links, schemas, amendments and
retractions, then prints the head. With `--expect-head`, it also confirms that a hash committed
in a published manifest is part of the chain. Exits `1` and names the record on any failure.

## Publishing

### `tkit publish --period P [--out dir] [--dry-run]`

`P` is `2026`, `2026-H1`, `2026-Q3` or an inclusive range such as `2026-01-01..2026-03-31`.

Publishing verifies the ledger first and refuses to continue if it is broken. It then aggregates
the period, renders every output, signs the manifest and uploads to
`reports/<period>/v<N>/`. Versions are never overwritten: publishing the same period again
creates `v2`, linked to `v1`. The stable `latest/` copy and `latest.json` move only when the
period is the newest one published.

| Flag | Meaning |
|---|---|
| `--out dir` | Publish to a local directory instead of the configured public store |
| `--dry-run` | Compute and print the summary, write nothing |

## Verifying and serving

### `tkit verify --pubkey key.pub <source> [reports/<label>/v<N>] [--chain]`

`<source>` is an `https://` base URL or a store URL (`file://`, `sos://`, `s3://`). Without a
path it verifies the publication `latest.json` points to. `--chain` also verifies every earlier
publication through the manifest links. See [Verifying a publication](verifying.md).

### `tkit serve [--addr :8080] [--store url]`

Serves the public store read-only over HTTP, mapping directories to `index.html`. Useful because
Exoscale SOS has no static-website hosting.

## Store URLs

| Scheme | Example |
|---|---|
| `file://` | `file://./ledger` |
| `sos://` | `sos://acme-ledger/prefix?zone=ch-gva-2` (Exoscale SOS) |
| `s3://` | `s3://bucket/prefix?endpoint=https://s3.example.com&region=eu-1` |

Credentials come from `EXOSCALE_API_KEY` and `EXOSCALE_API_SECRET`, or from
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. They are never read from the configuration
file.
