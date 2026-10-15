# Operating transparencykit

## Roles and keys

| Item | Where it lives | Who |
|---|---|---|
| `transparencykit.key` (Ed25519 private) | Offline or CI secret store; **never** in the ledger bucket | Publisher |
| `transparencykit.pub` | Website, contracts, DNS TXT, repository README (out-of-band trust anchor) | Everyone |
| Ledger IAM key | Scoped to the ledger bucket, PUT/GET/LIST only (no DELETE) | Recorder (Legal / T&S), publisher (read) |
| Public IAM key | Scoped to the public bucket | Publisher |

Give each store its own key through the environment; `publish` reads the ledger and writes the
public bucket in one run:

```sh
export TRANSPARENCYKIT_LEDGER_ACCESS_KEY=EXO…  TRANSPARENCYKIT_LEDGER_SECRET_KEY=…
export TRANSPARENCYKIT_PUBLIC_ACCESS_KEY=EXO…  TRANSPARENCYKIT_PUBLIC_SECRET_KEY=…
```

A store without its own pair falls back to `EXOSCALE_API_KEY` / `EXOSCALE_API_SECRET`, then to
the `AWS_*` variables.

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

Check a record or a batch without writing: `record validate gov_request -f case.json`,
`record import -f batch.json --check`. An import validates every item, including amendment and
retraction references, before it writes the first one. If it stops halfway (network, storage),
the error names the items already appended and the `--from N` to resume with.

`record list --effective` shows each record as it counts in reports, with amendments applied and
retractions marked.

Record the request when it is **received**, not when it's closed. Use `outcome: pending` and
amend later. That way the ledger timestamps prove the request was logged contemporaneously.

## Publishing cadence

| What | When |
|---|---|
| Data Act Art. 28 page | On every infrastructure/measures change: `publish --period <current year>`. `latest/data-act-art28.html` is the stable URL to cite in contracts. |
| Transparency report | Half-yearly (`2026-H1`, `2026-H2`) or yearly. |
| DSA Art. 15 | Yearly: `publish --period 2026` by end of February 2027 (two months after period end). |
| Corrections | Re-run `publish` for the same period. This creates `v2`, linked to `v1`. |

Publishing during a period (say the full year 2026 in October) counts everything recorded so far,
and takes infrastructure, measures and subprocessors as of today; later changes appear as
announced. Pass `--as-of YYYY-MM-DD` to choose another date within the period.

### Review before publishing

```sh
tkit publish --period 2026 --draft review-2026        # renders locally, signs nothing
# review review-2026/index.html and the "withheld" list printed by the command;
# fill in the Answer column of review-2026/dsa/tab11_qualitative.csv
tkit publish --from-draft review-2026                 # signs and uploads exactly that
```

Put recurring DSA qualitative answers in a JSON file named by `dsa_qualitative` in the config
(keys `own_initiative`, `automated_means`, `training`, `other`) so drafts start filled in. The
published files are re-rendered from the ledger state the draft was made from and must match the
draft byte for byte, except the answers. If someone published in the meantime, make a new draft.

### Interrupted publication

Uploads go files → `manifest.json` → `manifest.sig` → `latest/` → `latest.json`. Just run the
same `publish` again:

- stopped before `manifest.sig`: the folder is abandoned; the retry uses the next version number
  and logs the folder, which you may delete;
- stopped after `manifest.sig`: the retry finds the signed publication missing from
  `latest.json`, links it back in and continues the history from it.

### Archive and monitor

Run `transparencykit ledger verify` in CI daily, and archive every `manifest.json` you publish
somewhere you don't control (e.g. send it to your auditor or a public repository). Third-party
copies make silent rewrites impossible to hide. Auditors and customers can automate this with
`tkit verify --checkpoint seen.json …`, which fails if a publication they saw earlier disappears
from the history.

## Incident: key compromise or key rotation

Each manifest names the key that signed it (`key_id`), and `verify` accepts several trusted keys,
so a history signed by two keys stays verifiable.

1. Generate the new pair under a new name: `tkit keygen --out transparencykit-2027.key`
   (`keygen` refuses an existing file or an `--out` ending in `.pub`). Point `signing_key` at it.
2. **Keep the old public key.** Publish both out-of-band, in one PEM file or as two files, with
   the date of the switch and, for a compromise, the date from which the old key is no longer
   trusted:
   `cat transparencykit.pub transparencykit-2027.pub > transparencykit-keys.pem`.
3. Republish the current period. The new publication links to the previous one, so the history
   continues.
4. Tell verifiers to pass both keys:
   `tkit verify --pubkey transparencykit-keys.pem --chain <url>`.

Limitations: nothing cryptographic ties the new key to the old one yet; verifiers must learn of
the new key out-of-band. After a **compromise**, publications signed with the old key after the
compromise date cannot be told apart from forgeries by the signature alone. Compare them with
manifests archived by third parties or checkpoints, and republish affected periods with the new
key. Authenticated rotation (the new key announced in a manifest signed by the old one) is on the
roadmap.

## Incident: `publish` refuses because of a fork

`publish` refuses when a signed publication exists that the history in `latest.json` cannot reach,
for example after an older `latest.json` was restored from a backup. Find the newest signed
publication (`tkit verify --chain` against each `reports/<label>/v<N>/` shows which link
together), write its path and SHA-256 into `chain_head` of `latest.json`, and run `publish`
again. Never delete a signed publication to make the error go away: verifiers with a checkpoint
will flag the history as rewritten.

## Incident: `ledger verify` fails

Do not publish. Restore the affected object from its previous version (versioning is implied by
object lock), then investigate who wrote it using the bucket's IAM key audit.
