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
OpenSSL. The public key is written next to the private key with a `.pub` extension. `--out` names
the private key: a name ending in `.pub` is rejected, and existing files are never overwritten.

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

### `tkit record validate <type> [-f file] [--amends N]`

Runs every check `record add` runs, including the amendment or retraction reference, and prints
the sequence number the record would get. Writes nothing.

### `tkit record import -f records.json [--from N] [--check]`

Appends a JSON array of `{"type", "body", "amends"?, "by"?}` objects. Every item is validated
before the first one is written, including `amends` and retraction references. An item may refer
to an earlier item of the same batch by the sequence number it will receive.

| Flag | Meaning |
|---|---|
| `--check` | Validate the batch and print the sequence numbers it would get; write nothing |
| `--from N` | Start at item N (1-based). After an interrupted import, the error message names the N to resume with |

### `tkit record retract <seq> --reason "…"`

Appends a retraction. The retracted record and its amendments are excluded from publications;
the original stays in the ledger.

### `tkit record list [--type t] [--effective] [--json]` and `tkit record show <seq>`

Operator views of the ledger. They include `private` fields, so do not share their output.
`--effective` lists each original record once, as reports count it: the latest amendment applied,
a status of `original`, `amended by #N` or `RETRACTED by #N (not counted)`.

## Ledger

### `tkit ledger verify [--expect-head sha256:…]`

Recomputes every record hash, checks sequence numbers, `prev` links, schemas, amendments and
retractions, then prints the head. With `--expect-head`, it also confirms that a hash committed
in a published manifest is part of the chain. Exits `1` and names the record on any failure.

## Publishing

### `tkit publish --period P [--as-of date] [--out dir] [--dry-run] [--draft dir]`

`P` is `2026`, `2026-H1`, `2026-Q3` or an inclusive range such as `2026-01-01..2026-03-31`.

Publishing verifies the ledger first and refuses to continue if it is broken. It then aggregates
the period, renders every output, signs the manifest and uploads to
`reports/<period>/v<N>/`. Versions are never overwritten: publishing the same period again
creates `v2`, linked to `v1`. The stable `latest/` copy and `latest.json` move only when the
period is the newest one published. An interrupted run is repaired by running it again
([Operations](OPERATIONS.md#interrupted-publication)).

| Flag | Meaning |
|---|---|
| `--as-of date` | Date of current infrastructure, measures and subprocessors. Default: today, capped at the period end |
| `--out dir` | Publish to a local directory instead of the configured public store |
| `--dry-run` | Compute and print the summary, write nothing |
| `--draft dir` | Render everything into `dir` for review; sign and upload nothing |

The summary lists breakdowns withheld because their small counts could be calculated
([Specification §7.2](SPEC.md)).

### `tkit publish --from-draft dir`

Signs and uploads a reviewed draft. Only the Answer column of `dsa/tab11_qualitative.csv` may be
edited; every other file must match what the ledger state recorded in the draft produces.

## Verifying and serving

### `tkit verify --pubkey key.pub <source> [reports/<label>/v<N>] [--chain] [--checkpoint file]`

`<source>` is an `https://` base URL or a store URL (`file://`, `sos://`, `s3://`). Without a
path it verifies the publication `latest.json` points to and the `latest/` copies of it.

| Flag | Meaning |
|---|---|
| `--pubkey file` | Trusted public key, obtained out-of-band. Repeat it, or put several PEM blocks in one file, to cover a key rotation |
| `--chain` | Verify the whole history from the newest publication of any period |
| `--checkpoint file` | Remember publications across runs and fail if one disappears or changes; implies `--chain` |

See [Verifying a publication](verifying.md).

### `tkit serve [--addr :8080] [--store url]`

Serves the public store read-only over HTTP, mapping directories to `index.html`. Useful because
Exoscale SOS has no static-website hosting.

## Store URLs

| Scheme | Example |
|---|---|
| `file://` | `file://./ledger` |
| `sos://` | `sos://acme-ledger/prefix?zone=ch-gva-2` (Exoscale SOS) |
| `s3://` | `s3://bucket/prefix?endpoint=https://s3.example.com&region=eu-1` |

Credentials are never read from the configuration file. Each store takes, in order:

| Store | Variables |
|---|---|
| Ledger | `TRANSPARENCYKIT_LEDGER_ACCESS_KEY`, `TRANSPARENCYKIT_LEDGER_SECRET_KEY` |
| Public (`publish`, `serve`, `verify` with a store URL) | `TRANSPARENCYKIT_PUBLIC_ACCESS_KEY`, `TRANSPARENCYKIT_PUBLIC_SECRET_KEY` |
| Fallback for both | `EXOSCALE_API_KEY`, `EXOSCALE_API_SECRET`, then `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` |
