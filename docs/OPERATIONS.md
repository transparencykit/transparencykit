# Operating transparencykit

## Roles and keys

| Item | Where it lives | Who |
|---|---|---|
| `transparencykit.key` (Ed25519 private) | Offline or CI secret store; **never** in the ledger bucket | Publisher |
| `transparencykit.pub` | Website, contracts, DNS TXT, repository README (out-of-band trust anchor) | Everyone |
| Ledger IAM key | Scoped to the ledger bucket, PUT/GET/LIST only (no DELETE) | Recorder (Legal / T&S) |
| Public IAM key | Scoped to the public bucket | Publisher |

Use a **single writer** for the ledger unless you've confirmed your backend honours
`If-None-Match` (run `TestSOSIntegration`, which reports it).

## Buckets (Exoscale SOS)

The full list of prerequisites, with commands for the MinIO client and the AWS CLI, is in
[Storage requirements](storage.md). In short:

1. Ledger bucket **with object lock**. It can only be enabled at creation time (Exoscale docs).
   Set a default retention:
   `mc retention set --default GOVERNANCE 3650d sos/<ledger>`. COMPLIANCE mode cannot be
   shortened by anyone; choose it only after a dry run.
2. Optionally set `ledger_retention` in `transparencykit.json` so every record also carries
   per-object retention headers.
3. Public bucket without lock. transparencykit uploads with a `public-read` ACL.

## Recording workflow

```sh
transparencykit record template gov_request > case.json   # fill in; keep case refs in "private"
transparencykit record add gov_request -f case.json --by legal@acme.ch
# later, when the outcome is known:
transparencykit record add gov_request -f case-final.json --amends 42
# mistakes are never deleted, only retracted:
transparencykit record retract 42 --reason "duplicate of 41"
```

Record the request when it is **received**, not when it's closed. Use `outcome: pending` and
amend later. That way the ledger timestamps prove the request was logged contemporaneously.

## Publishing cadence

| What | When |
|---|---|
| Data Act Art. 28 page | On every infrastructure/measures change: `publish --period <current year>`. `latest/data-act-art28.html` is the stable URL to cite in contracts. |
| Transparency report | Half-yearly (`2026-H1`, `2026-H2`) or yearly. |
| DSA Art. 15 | Yearly: `publish --period 2026` by end of February 2027 (two months after period end). |
| Corrections | Re-run `publish` for the same period. This creates `v2`, linked to `v1`. |

Run `transparencykit ledger verify` in CI daily, and archive every `manifest.json` you publish
somewhere you don't control (e.g. send it to your auditor or a public repository). Third-party
copies make silent rewrites impossible to hide.

## Incident: key compromise

Generate a new key, publish the new `transparencykit.pub` out-of-band, and republish the current period.
Announce the old key ID as revoked. Signed key rotation is planned for v0.2.

## Incident: `ledger verify` fails

Do not publish. Restore the affected object from its previous version (versioning is implied by
object lock), then investigate who wrote it using the bucket's IAM key audit.
