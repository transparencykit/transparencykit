# Verifying a publication

You do not need to trust a provider's transparency report. If it was published with
TransparencyKit, you can check it yourself.

## What you need

1. The provider's **public key** (`transparencykit.pub`), from a source you trust: their main
   website, their contract or DPA, or a DNS record. A copy is served next to the reports for
   convenience, but a key fetched from the same place as the reports proves only that the files
   are internally consistent, not who signed them.
2. The **base URL** of their published reports.
3. The `transparencykit` (or `tkit`) binary.

## Run the check

```sh
tkit verify --pubkey acme.pub --chain https://transparency.acme.example
```

Example output:

```text
ok  reports/2026/v2/manifest.json  (2026 v2, 14 files, ledger #1234)
ok  reports/2026/v1/manifest.json  (2026 v1, 14 files, ledger #1180)
verified 2 publication(s) with key ed25519:2f3f8628f71f2838
```

| Exit code | Meaning |
|---|---|
| `0` | Every signature and every file matched. |
| `1` | Verification failed. The message names the file or link that does not match. |
| `2` | Usage or network error; nothing was concluded. |

## Try it on the example

The website publishes a signed report for **So Sovereign Cloud SA**, a fictional provider. Its
public key is in the repository:

```sh
curl -sO https://raw.githubusercontent.com/transparencykit/transparencykit/main/examples/so-sovereign-cloud/transparencykit.pub
tkit verify --pubkey transparencykit.pub --chain https://transparencykit.github.io/transparencykit/example
```

Then [open the report](https://transparencykit.github.io/transparencykit/example/latest/index.html)
and its [Data Act page](https://transparencykit.github.io/transparencykit/example/latest/data-act-art28.html).

## What a pass proves

- **The files are the ones the provider signed.** Every published file is listed in
  `manifest.json` with its SHA-256 hash and size, and the manifest is signed with Ed25519.
- **Past reports were not quietly rewritten.** Each manifest records the hash of the previous
  one. With `--chain`, the check walks back to the first publication. A correction appears as a
  new version, never as an edit.
- **The numbers come from one specific ledger state.** Each manifest commits to the hash of the
  provider's private ledger. An auditor with ledger access can confirm it with
  `tkit ledger verify --expect-head <hash>`.

## What it cannot prove

A signature cannot show that a provider recorded every request it received. TransparencyKit
makes the recorded history tamper-evident; completeness still rests on the provider's process
and on external audit. Keep your own copies of manifests you have checked: they make any later
rewrite detectable.

## Small numbers

Breakdowns show counts between 1 and the provider's threshold (by default 1–2) as a range, to
avoid identifying individual customers. Zero is always shown exactly, so a statement such as "0
disclosures to third-country authorities" is checkable. The DSA CSV files are always exact.
