# TransparencyKit — Specification v0.1

Status: draft · Date: 2026-10-14 · Licence: Apache-2.0

TransparencyKit is a single static binary that lets a cloud, SaaS, PaaS or IaaS provider keep a
**tamper-evident ledger** of transparency-relevant events in S3-compatible object storage
and **publish signed, verifiable transparency outputs** from it.

> "Proof, not prose": every published number is derived from a hash-chained private
> ledger, and every published file is covered by a signed manifest that anyone can check.

---

## 1. Goals and non-goals

### Goals

| # | Goal |
|---|------|
| G1 | Append-only, hash-chained ledger of events: government/authority requests, abuse notices, subprocessor changes, infrastructure jurisdiction, access-protection measures. |
| G2 | State lives only in object storage (Exoscale SOS or any S3-compatible store; local directory for development). No database. |
| G3 | One static binary, zero third-party Go dependencies (auditable supply chain). |
| G4 | Publish from one source: (a) aggregated transparency report (HTML + JSON), (b) EU Data Act Art. 28 disclosure page (HTML + JSON), (c) DSA Art. 15 hosting-provider data aligned with the Annex I template of Implementing Regulation (EU) 2024/2835 (CSV), (d) subprocessor list (JSON) and change feed (Atom). |
| G5 | Every publication is covered by an Ed25519-signed manifest; manifests form a public hash chain so silent rewrites of past reports are detectable. |
| G6 | Raw case data never leaves the private ledger; published breakdowns apply small-count banding. |

### Non-goals (v0.1)

- Submitting statements of reasons to the DSA Transparency Database. Only online platforms owe these; hosting services do not.
- Content-moderation workflow, case management or ticketing. TransparencyKit records outcomes; it does not run the process.
- Legal advice. Templates and categorisations must be reviewed by counsel before filing.
- Witness co-signing / public transparency-log anchoring (planned v0.3, see §11).

---

## 2. Regulatory mapping

| Output | Legal hook | What TransparencyKit produces |
|---|---|---|
| Data Act page | Reg. (EU) 2023/2854 Art. 28(1)(a): jurisdiction of the ICT infrastructure per service; Art. 28(1)(b): general description of technical, organisational and contractual measures against conflicting international governmental access. Art. 28(2): page must be referenced in contracts. | `data-act-art28.html` / `.json`: per-zone jurisdiction table from `infrastructure` records, measures from the latest `measures` record. Stable URL `latest/data-act-art28.html`. |
| Customer notice evidence | Data Act Art. 32(5): inform the customer of a third-country authority request before complying (law-enforcement exception). | `customer_notification` field on every `gov_request`; report shows notification stats for third-country requests. |
| DSA report | Reg. (EU) 2022/2065 Art. 15; Implementing Reg. (EU) 2024/2835 (templates applicable from 1 July 2025; first full cycle 1 Jan–31 Dec 2026; publish within two months of period end). | `dsa/` CSVs: tab 1 identification, tab 2 categories, tab 3 Member State orders (Art. 9 and Art. 10), tab 4 notices (Art. 16), tab 5 own-initiative (illegal content), tab 6 own-initiative (T&C), tab 11 qualitative (skeleton). Semicolon-delimited. |
| Government-request report | Voluntary; structure follows Ranking Digital Rights indicator P11a (breakdown by country, legal authority, content vs non-content, compliance rate; structured data export). | `index.html` + `report.json`. |
| Subprocessor transparency | GDPR Art. 28(2)/(4) prior-notice practice. | `subprocessors.json` + `subprocessors.atom`. |

**Template fidelity caveat.** The DSA CSV column headers are aligned with published hosting-provider
filings of the Annex I template (tabs and column semantics), but v0.1 is **not** validated
byte-for-byte against the official XLSX. Operators must map/verify against the Commission's
current template before filing. Median times are expressed in **hours**.

---

## 3. Architecture

```
            operator workstation / CI                     object storage (SOS)
 ┌──────────────────────────────────────┐        ┌───────────────────────────────────┐
 │ transparencykit record add  ───────────────► │──PUT──►│ private bucket (object lock ON)   │
 │ transparencykit ledger verify ◄───────────── │◄──GET──│   records/000000000001.json …     │
 │ transparencykit publish ──┐                  │        │   head.json  (advisory)           │
 │   aggregate+band  │  sign (Ed25519)  │        └───────────────────────────────────┘
 │   render          ▼                  │        ┌───────────────────────────────────┐
 │                manifest.json+.sig ───│──PUT──►│ public bucket (public-read ACL)   │
 └──────────────────────────────────────┘        │   reports/2026/v1/…  latest/…     │
                                                 │   latest.json  index.html  .pub   │
 anyone: transparencykit verify https://…  ◄────GET──────│                                   │
 browser: direct object URLs or `transparencykit serve`  └───────────────────────────────────┘
```

SOS has no static website hosting and no event notifications. Public files are read through
direct object URLs (`https://<bucket>.sos-<zone>.exo.io/<key>`) or through `transparencykit serve`.
The operator runs `publish` on a schedule; nothing reacts to events.

---

## 4. Ledger format

### 4.1 Canonical JSON

All hashing uses **canonical JSON**: UTF-8, object keys sorted lexicographically by byte,
no insignificant whitespace, no HTML escaping, integers only (floats are rejected in ledger
records), strings escaped per Go `encoding/json`. This matches RFC 8785 for the value types
TransparencyKit allows.

### 4.2 Entry envelope

Each ledger entry is one object at `records/<seq zero-padded to 12>.json`:

```json
{
  "v": 1,
  "seq": 42,
  "type": "gov_request",
  "recorded_at": "2026-10-14T20:15:03Z",
  "recorded_by": "legal@example.ch",
  "amends": 0,
  "prev": "sha256:<64 hex of entry 41's hash>",
  "body": { "...": "type-specific, see §5" },
  "hash": "sha256:<64 hex>"
}
```

- `hash = "sha256:" + hex(SHA-256(canonical(envelope without "hash")))`.
- Genesis `prev` is `sha256:` followed by 64 zeros.
- `seq` starts at 1 and is contiguous.
- `amends` (optional, omitted when 0) is the `seq` of an earlier entry **of the same type** that this
  entry supersedes. It must point to an original (non-amending) entry. The latest amendment wins.
- `recorded_by` is optional free text (operator identity).
- Unknown envelope or body fields are rejected.

### 4.3 Append protocol

1. List `records/` and find the highest `seq` *n* (the listing is authoritative; `head.json` is a cache).
2. Fetch entry *n*, recompute and check its hash.
3. Build entry *n+1* with `prev = hash(n)` and validate the body.
4. `PUT records/<n+1>` with `If-None-Match: *`. On `412 Precondition Failed` (another writer won),
   restart from 1, up to 5 attempts.
5. Best-effort `PUT head.json` `{"seq":n+1,"hash":…,"updated_at":…}`.

**Concurrency caveat.** Exoscale does not document SOS support for conditional writes. Without it,
two simultaneous writers can race. Mitigations: run a **single writer** (recommended), keep
bucket versioning on (object lock implies it) so an overwritten entry survives as a prior version,
and run `ledger verify` after writes. v0.2 adds `ledger verify --versions` (ListObjectVersions).

### 4.4 Immutability

Create the private bucket **with object lock** (it cannot be enabled later on SOS) and a default
retention (e.g. GOVERNANCE, 10 years). Optionally TransparencyKit sets per-object retention headers
(`x-amz-object-lock-mode`, `x-amz-object-lock-retain-until-date`, plus `Content-MD5`) when
`ledger_retention` is configured. Scope a dedicated IAM key to the bucket.

### 4.5 Verification (`transparencykit ledger verify`)

Fails if any of these hold:

- a gap or duplicate in `seq`;
- a stored `hash` that doesn't match the recomputed hash;
- a `prev` that doesn't match the previous hash;
- a body that fails schema validation;
- an `amends` target that is missing, of another type, or itself an amendment.

It prints the head `seq` and `hash`. `--expect-head sha256:…` additionally asserts that the hash
committed in a published manifest (`ledger.hash`) is an entry of the verified chain. With it, an
auditor who has access to the ledger can prove a report was derived from a prefix of exactly this
ledger, even after later appends.

---

## 5. Record types (body schemas)

Dates are `YYYY-MM-DD`, timestamps are RFC 3339 UTC. Countries are ISO 3166-1 alpha-2
(`GB` not `UK`, `GR` not `EL`). Every body may carry a `private` object
(`{"case_ref": "...", "notes": "..."}`), which is **never** read by `publish`.

### 5.1 `gov_request`: request or order from a public authority

| Field | Type | Req. | Values / notes |
|---|---|---|---|
| `received_at` | timestamp | ✓ | Drives the reporting period. |
| `authority_country` | ISO2 | ✓ | Country of the requesting authority. |
| `authority_type` | enum | ✓ | `law_enforcement`, `court`, `intelligence`, `regulator`, `other` |
| `legal_basis` | string | ✓ | e.g. `CH StPO Art. 265`, `US 18 U.S.C. § 2713 (CLOUD Act)`, `DSA Art. 9` |
| `kind` | enum | ✓ | `user_data`, `content_action`, `preservation`, `emergency`, `other` |
| `dsa_order` | enum | | `art9` (order to act against illegal content) or `art10` (order to provide information). Only valid when `authority_country` is an EU Member State. |
| `illegal_content_category` | enum | if `art9` | DSA category constant (§5.5). |
| `data_category` | enum | | `none`, `non_content`, `content`, `both` |
| `accounts` | int ≥ 0 | | Accounts/customers specified. |
| `items` | int ≥ 0 | | Specific items of information (DSA tab 3). Default 1. |
| `outcome` | enum | ✓ | `pending`, `disclosed_full`, `disclosed_partial`, `actioned`, `rejected`, `withdrawn`, `no_data` |
| `acknowledged_at` | timestamp | | Authority informed of receipt (DSA median). |
| `completed_at` | timestamp | | Effect given / response sent (DSA median). |
| `customer_notification` | enum | ✓ | `notified_before`, `notified_after`, `not_notified_legal_prohibition`, `not_notified_other`, `not_applicable` |
| `service`, `zone` | string | | |

A request is **third-country** when `authority_country` is not in the configured
`home_jurisdictions` (default `["CH","EU","EEA"]`). This is derived at publish time, and the set
used is written into `report.json`.

### 5.2 `abuse_notice`: notice or own-initiative detection (DSA Art. 16 / own initiative)

| Field | Type | Req. | Values |
|---|---|---|---|
| `received_at` | timestamp | ✓ | |
| `source` | enum | ✓ | `art16_notice`, `trusted_flagger`, `own_initiative`, `other_notification` |
| `category` | enum | ✓ | DSA category constant (§5.5) |
| `basis` | enum | ✓ | `law`, `terms`, `none` |
| `action` | enum | ✓ | `none`, `removed`, `disabled`, `restricted`, `account_suspended`, `account_terminated`, `forwarded_to_customer` |
| `actioned_at` | timestamp | | Median time to action. |
| `automated_detection` | bool | | Detected by automated means. |
| `automated_decision` | bool | | Decided by automated means. |
| `items` | int ≥ 1 | | Default 1. |
| `service` | string | | |

### 5.3 `subprocessor_change`

| Field | Req. | Notes |
|---|---|---|
| `effective` | ✓ | date |
| `change` | ✓ | `add`, `update`, `remove` |
| `name` | ✓ | Fold key (case-sensitive). |
| `purpose` | add/update | |
| `locations` | add/update | ISO2 list |
| `services` | | list |
| `notice_given` | | date the customers were notified |

### 5.4 `infrastructure` and `measures`

`infrastructure` (fold key `zone`; latest `effective` ≤ period end wins; `status: retired` removes it):
`effective`, `zone`, `country`, `city`, `operator`, `operator_country`, `legal_entity`,
`legal_entity_country`, `services[]`, `status` (`active`|`retired`).

`measures` (latest `effective` ≤ period end wins): `effective`, `statement`, `technical[]`,
`organisational[]`, `contractual[]`.

### 5.5 Category taxonomy

These are the DSA Transparency Database `STATEMENT_CATEGORY_*` constants, which the Commission aligned
with the Annex I template:
`ANIMAL_WELFARE, CONSUMER_INFORMATION, CYBER_VIOLENCE, CYBER_VIOLENCE_AGAINST_WOMEN,
DATA_PROTECTION_AND_PRIVACY_VIOLATIONS, ILLEGAL_OR_HARMFUL_SPEECH, INTELLECTUAL_PROPERTY_INFRINGEMENTS,
NEGATIVE_EFFECTS_ON_CIVIC_DISCOURSE_OR_ELECTIONS, NOT_SPECIFIED_NOTICE, OTHER_VIOLATION_TC,
PROTECTION_OF_MINORS, RISK_FOR_PUBLIC_SECURITY, SCAMS_AND_FRAUD, SELF_HARM,
UNSAFE_AND_PROHIBITED_PRODUCTS, VIOLENCE`. These are stored with the `STATEMENT_CATEGORY_` prefix.
Keyword sub-categories (`KEYWORD_*`) are out of scope for v0.1.

### 5.6 `retraction`

`{"seq": N, "reason": "..."}` excludes entry N (and its amendments) from aggregation. The
original entry stays in the ledger.

---

## 6. Configuration (`transparencykit.json`)

```json
{
  "provider": { "name": "Example Cloud SA", "legal_entity": "Example Cloud SA",
                "country": "CH", "contact": "transparency@example.ch",
                "website": "https://example.ch", "service_type": "provider of hosting services" },
  "home_jurisdictions": ["CH", "EU", "EEA"],
  "ledger": "sos://example-ledger/transparencykit?zone=ch-gva-2",
  "public": "sos://example-transparency/?zone=ch-gva-2",
  "public_base_url": "https://example-transparency.sos-ch-gva-2.exo.io",
  "signing_key": "transparencykit.key",
  "min_count": 3,
  "ledger_retention": { "mode": "GOVERNANCE", "days": 3650 }
}
```

Store URL schemes:

| Scheme | Example | Notes |
|---|---|---|
| `file://` | `file://./ledger` | Local directory. Atomic create via `O_EXCL`. |
| `sos://` | `sos://bucket/prefix?zone=ch-gva-2` | Endpoint `https://sos-<zone>.exo.io`, region `<zone>`, path-style. |
| `s3://` | `s3://bucket/prefix?endpoint=https://…&region=…` | Any S3-compatible store. |

Credentials are read from `EXOSCALE_API_KEY` / `EXOSCALE_API_SECRET`, falling back to
`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`. They are never read from the config file.

---

## 7. Publication

`transparencykit publish --period 2026 | 2026-H1 | 2026-H2 | 2026-01-01..2026-03-31`

### 7.1 Aggregation rules

- An entry is in the period when its `received_at` / `effective` falls in it (inclusive dates, UTC).
- Amendments are applied, then retractions.
- State-like types (`subprocessor_change`, `infrastructure`, `measures`) are folded up to the
  period end. Changes within the period are listed separately.
- Medians use the lower median for even counts and are expressed in hours with 1 decimal. Pairs
  missing an end timestamp are excluded and counted as `pending`.

### 7.2 Small-count banding (privacy)

In `index.html` and `report.json`, a **breakdown cell** whose count *c* satisfies
`0 < c < min_count` is published as the string `"1-<min_count-1>"`. Zero is always published
exactly, so explicit "0 third-country disclosures" claims stay possible. Overall totals are exact.
Residual risk: subtracting other cells from a total can reveal a single banded cell. Choose
`min_count` and the breakdown granularity accordingly. **DSA CSVs are never banded**, because the
DSA requires exact figures.

### 7.3 Output layout (public store)

```
transparencykit.pub                         Ed25519 public key (PEM), convenience copy only
latest.json                         unsigned pointer, see below
index.html                          list of all published reports (convenience)
reports/<label>/v<N>/
    index.html                      human-readable report
    report.json                     schema transparencykit/report/v1
    data-act-art28.html|json        as of period end
    subprocessors.json              current list as of period end
    subprocessors.atom              full change history up to period end
    dsa/tab1_identification.csv … tab11_qualitative.csv
    manifest.json                   signed manifest
    manifest.sig                    base64(Ed25519(manifest.json bytes))
latest/…                            copy of the most recent version (stable URLs for contracts)
```

`latest.json` holds `label`, `version`, `manifest`, `manifest_sha256` and `period_end` for the
publication with the latest period end. It also holds `chain_head` (`{path, sha256}`), the most
recently published manifest of any period. Publishing an older period (e.g. a correction to 2025)
extends the chain but does not move `latest/`. Before linking, `publish` re-hashes the chain-head
manifest and refuses to continue if it doesn't match `latest.json`.

Versions are immutable: republishing a period creates `v<N+1>`. This satisfies the DSA rule
that report versions be explicitly marked.

### 7.4 Manifest

```json
{
  "schema": "transparencykit/manifest/v1",
  "provider": "Example Cloud SA",
  "label": "2026", "version": 1,
  "period": { "start": "2026-01-01", "end": "2026-12-31" },
  "generated_at": "2027-02-10T09:00:00Z",
  "ledger": { "seq": 1234, "hash": "sha256:…" },
  "previous": { "path": "reports/2025/v2/manifest.json", "sha256": "…" },
  "key_id": "ed25519:<16 hex of SHA-256(public key DER)>",
  "files": [ { "path": "index.html", "sha256": "…", "size": 12345 } ]
}
```

`previous` links to `latest.json`'s `chain_head` from before this publication, which forms a hash
chain over all publications in publication order.

---

## 8. Public verification (`transparencykit verify`)

`transparencykit verify --pubkey transparencykit.pub <base-url | store-url> [reports/<label>/v<N>]`

1. Resolve the target via `latest.json` if no path is given.
2. Fetch `manifest.json` and `manifest.sig`, and verify the Ed25519 signature with the
   **out-of-band** public key. The key served next to the reports is only a convenience, and
   using it emits a warning.
3. Fetch every listed file and compare SHA-256 and size.
4. With `--chain`, follow `previous` links to the first publication, verifying each manifest's
   signature and its SHA-256 against the link.

Exit code 0 = verified; 1 = verification failure; 2 = usage or I/O error.

---

## 9. CLI

| Command | Purpose |
|---|---|
| `transparencykit init` | Write a `transparencykit.json` template, generate a key pair, and print bucket setup commands. |
| `transparencykit keygen --out transparencykit.key` | Ed25519 key pair (PKCS#8 / PKIX PEM, OpenSSL-compatible). |
| `transparencykit record template <type>` | Print a JSON skeleton. |
| `transparencykit record add <type> [-f file\|-] [--amends N] [--by who]` | Validate and append. |
| `transparencykit record import -f records.json` | Bulk append a JSON array of `{type, body, amends?, by?}`. Every item is validated before any write. |
| `transparencykit record retract <seq> --reason …` | Append a retraction. |
| `transparencykit record list [--type t] [--json]` / `record show <seq>` | Inspect (private fields shown — operator only). |
| `transparencykit ledger verify [--expect-head h]` | Full chain verification. |
| `transparencykit publish --period P [--out dir] [--dry-run]` | Aggregate, render, sign, upload. |
| `transparencykit verify …` | Public verification (§8). |
| `transparencykit serve [--addr :8080]` | Read-only HTTP server for the public store. |
| `transparencykit version` | |

---

## 10. Security model

| Threat | Mitigation |
|---|---|
| Operator silently edits or deletes a past record | Hash chain plus object lock (governance/compliance); `ledger verify`. |
| Operator republishes an altered past report | Immutable versions; manifest hash chain; third parties keep copies of earlier manifests. |
| Attacker replaces public files | Signature check against the out-of-band key. |
| Key compromise | Key rotation: a new key is announced in a manifest signed by the old key (v0.2); keep the key offline or in CI secrets. |
| Re-identification from small counts | `min_count` banding; private fields never rendered. |
| Supply chain | Zero third-party dependencies; reproducible `CGO_ENABLED=0 -trimpath` builds. |

Out of scope: a provider that never records a request in the first place. TransparencyKit makes
recorded history tamper-evident; it cannot prove completeness. Witness co-signing and
external auditors address this partially.

---

## 11. Roadmap

- **v0.2:** `ledger verify --versions`; key rotation; `canary.json`; XLSX export; official-template validation.
- **v0.3:** publish manifest checksums to a public transparency log (Sigsum or Tessera with C2SP witnesses).
- **v0.4:** importers (ticketing CSV, Lumen export); multi-signer approval (two-person rule).

---

## 12. Acceptance criteria (v0.1)

1. A tampered, deleted or reordered ledger entry makes `ledger verify` exit non-zero, naming the seq.
2. `publish` output passes `verify` over HTTP. Modifying any published byte makes it fail.
3. Republishing a period creates `v2`, whose manifest links `v1` by hash; `verify --chain` passes.
4. Counts 1..`min_count`−1 never appear in HTML/JSON breakdowns; zeros do appear; DSA CSVs are exact.
5. `private` fields never appear in any published file.
6. The S3 client signs requests correctly (AWS SigV4 reference vectors) and works against an S3-compatible endpoint, including pagination and `If-None-Match` conflicts.
7. `CGO_ENABLED=0 go build` produces a static binary with no third-party modules in `go.mod`.
