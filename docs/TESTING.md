# Test plan

`go test -race ./...` runs everything except the live SOS test. CI also builds a static binary.

| Layer | Package / file | What is proven |
|---|---|---|
| Canonical JSON | `internal/canon` | Key-order independence, no HTML escaping, floats and trailing data rejected |
| Schemas | `internal/record` | Every enum, date, country (`UK`→`GB`), DSA-order rule; templates valid; strict unknown-field rejection |
| Hash chain | `internal/record` | Detects edited body, edited and re-hashed body, deleted first/middle entry, reorder, edited timestamp; amendment and retraction rules |
| Ledger on stores | `internal/ledger` | Append/verify, `head.json`, invalid amendments refused, tampered head blocks append, gaps detected, **4 concurrent writers on S3 keep a valid chain**, backend without conditional writes is detected by verify |
| S3 client | `internal/store` | **AWS SigV4 reference vectors** (GET object, ListObjects); contract suite on file and fake S3 (pagination with page size 2, key prefix, unicode keys, create-only); lock/ACL/MD5 headers are sent and signed; URL parsing |
| Live SOS | `internal/store/sos_integration_test.go` | Contract against a real bucket; reports whether SOS honours `If-None-Match`. Opt-in via env. |
| Keys / signatures | `internal/manifest` | PEM round-trip, 0600 key file, no overwrite, tamper and wrong-key rejection, **OpenSSL interop** (`openssl pkeyutl -verify`) |
| Aggregation | `internal/report` | Period parsing and UTC boundaries, lower median, banding, third-country classification, amendments, subprocessor/infra/measures folding, retired zones |
| Privacy | `internal/report`, e2e | `private` content never appears in any published file |
| DSA CSVs | `internal/report` | Column counts match published hosting-provider filings (tab 3: 20, tab 4: 28), exact values, never banded |
| Publication chain | `internal/verify` | v1→v2 link, `latest/` verifiable, older-period publication extends the chain without moving `latest/`, byte tamper, **re-signed old manifest caught by link hash**, tampered chain head blocks publish, immutable versions, dry run writes nothing, HTTP verification |
| CLI end-to-end | `cmd/transparencykit/e2e_test.go` | Real binary: init → add/amend → list/show → verify → publish → verify (file, S3, HTTP), exit codes 0/1/2, `--expect-head`, tamper paths, **static linking** |

## Manual acceptance (before a release)

1. `tools/demo.sh --serve` and review `latest/index.html` and `latest/data-act-art28.html` on
   desktop and phone.
2. Open `dsa/*.csv` next to the Commission's current template and confirm the column mapping.
3. Run `TestSOSIntegration` against a scratch bucket in each target zone.
