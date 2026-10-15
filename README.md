# transparencykit

**Tamper-evident transparency ledger and signed transparency reports for cloud providers.**
One static binary, zero third-party dependencies, state in S3-compatible object storage
(built for Exoscale SOS).

Cloud providers make claims such as "we have never disclosed customer data to a foreign
government" or "our infrastructure is under Swiss jurisdiction". transparencykit turns those claims
into something customers, auditors and regulators can check:

- **Record** government/authority requests, third-country (e.g. CLOUD Act) requests, abuse
  notices, subprocessor changes and infrastructure jurisdiction in an append-only,
  hash-chained ledger (object-locked bucket).
- **Publish** from that one source:
  - an aggregated **transparency report** (HTML + JSON, small counts banded);
  - an **EU Data Act Art. 28** disclosure page (jurisdiction per zone + measures against
    international governmental access);
  - **DSA Art. 15** data for hosting providers, in CSVs following the Annex I template tabs of
    Implementing Regulation (EU) 2024/2835;
  - a **subprocessor list** (JSON) and **change feed** (Atom).
- **Sign** every publication with Ed25519. Manifests are hash-chained, so a silently rewritten
  past report is detectable.
- **Verify** anything, anytime: `transparencykit verify --pubkey transparencykit.pub https://…`.

Website and documentation: **https://transparencykit.github.io/transparencykit/**

> Status: v0.1 (draft). The spec is in [docs/SPEC.md](docs/SPEC.md). Not legal advice. Have
> counsel review categorisations and map DSA CSVs to the official template before filing.

## Quick start (local)

```sh
go install github.com/transparencykit/transparencykit/cmd/tkit@latest   # or build: go build ./cmd/transparencykit
tkit init                                   # transparencykit.json + transparencykit.key/.pub
tkit record template gov_request > req.json # edit it
tkit record add gov_request -f req.json --by legal@example.ch
tkit ledger verify
tkit publish --period 2026
tkit verify --pubkey transparencykit.pub --chain file://$PWD/public
tkit serve                                  # http://localhost:8080
```

`tools/demo.sh --serve` builds a full demo (fictional provider, 117 records) and serves it.

## On Exoscale SOS

```sh
export EXOSCALE_API_KEY=EXO… EXOSCALE_API_SECRET=…      # bucket-scoped IAM key recommended
mc alias set sos https://sos-ch-gva-2.exo.io "$EXOSCALE_API_KEY" "$EXOSCALE_API_SECRET"
mc mb --with-lock sos/acme-transparencykit-ledger              # object lock: only at creation time
mc retention set --default GOVERNANCE 3650d sos/acme-transparencykit-ledger
mc mb sos/acme-transparency
```

```json
"ledger": "sos://acme-transparencykit-ledger/?zone=ch-gva-2",
"public": "sos://acme-transparency/?zone=ch-gva-2",
"public_base_url": "https://acme-transparency.sos-ch-gva-2.exo.io"
```

Object lock can only be enabled when the bucket is created. The full list of prerequisites
(object lock, retention, ACLs, conditional writes, API keys) is in
[docs/storage.md](docs/storage.md).

SOS has no static website hosting. Readers use direct object URLs
(`…/latest/index.html`) or `transparencykit serve`. See [docs/OPERATIONS.md](docs/OPERATIONS.md).

## Commands

The binary is `transparencykit`; `tkit` is the same program under a short name.

| Command | |
|---|---|
| `init`, `keygen` | Config template, Ed25519 key pair (OpenSSL-compatible PEM) |
| `record template/add/import/retract/list/show` | Write and inspect the ledger (amendments via `--amends N`) |
| `ledger verify [--expect-head h]` | Full chain check; prove a manifest's ledger head is in the chain |
| `publish --period 2026\|2026-H1\|2026-Q3\|A..B` | Aggregate, render, sign, upload (immutable `v<N>` + `latest/`) |
| `verify --pubkey k.pub <url> [path] [--chain]` | Check signatures, file hashes and the manifest chain |
| `serve` | Read-only HTTP server for the public store |

Exit codes: `0` OK, `1` verification failure, `2` usage or I/O error.

## Design choices

- **Zero dependencies.** Standard library only. The S3 client and SigV4 signer are about 400
  lines and are tested against the AWS reference vectors. The supply chain is auditable.
- **Integers only, canonical JSON.** Record hashes are reproducible in any language (RFC 8785
  subset).
- **Private stays private.** `private.case_ref`/`notes` never reach a published file. A test
  enforces this.
- **Banding.** Breakdown counts 1..`min_count`−1 are published as ranges. Zeros are exact, so
  "0 third-country disclosures" is a checkable statement. DSA CSVs are exact because the law
  requires it.

## Development

```sh
go test -race ./...          # unit, integration (fake S3) and end-to-end (real binary) tests
TRANSPARENCYKIT_SOS_TEST_BUCKET=… TRANSPARENCYKIT_SOS_TEST_ZONE=ch-gva-2 go test ./internal/store -run SOS -v
```

See [docs/TESTING.md](docs/TESTING.md) for the test plan and [AGENTS.md](AGENTS.md) for the
code map.

## Website

The site in `tools/site/` renders `docs/*.md` together with the landing page. GitHub Actions
(`.github/workflows/pages.yml`) builds and deploys it to GitHub Pages on every push to `main`
that touches `docs/` or `tools/site/`. Build locally with:

```sh
cd tools/site && npm ci && node build.mjs   # output in ../../_site
```

## Licence

Copyright 2026 The TransparencyKit Authors. Licensed under the Apache License, Version 2.0; see
[LICENSE](LICENSE).
