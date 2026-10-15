# Getting started

This walk-through runs entirely on your machine with local directories. It takes about five
minutes and needs nothing but the binary. When you are ready for real storage, continue with
[Operations](OPERATIONS.md).

## 1. Install

Download a release binary from
[GitHub releases](https://github.com/transparencykit/transparencykit/releases), or build from
source with Go 1.24 or later:

```sh
go install github.com/transparencykit/transparencykit/cmd/transparencykit@latest
go install github.com/transparencykit/transparencykit/cmd/tkit@latest   # optional short name
```

`tkit` is the same program under a shorter name. The examples below use it.

## 2. Create a workspace

```sh
mkdir acme-transparency && cd acme-transparency
tkit init
```

`init` writes three files:

| File | Purpose |
|---|---|
| `transparencykit.json` | Configuration: who you are, where the ledger and public files live |
| `transparencykit.key` | Your Ed25519 signing key. Keep it secret. |
| `transparencykit.pub` | The matching public key. Publish it widely. |

Open `transparencykit.json` and set your provider name and contact. The defaults store the
ledger in `./ledger` and published files in `./public`.

## 3. Describe your infrastructure

Every record starts from a template:

```sh
tkit record template infrastructure > zone.json
```

Edit `zone.json` (zone, country, legal entity, data-centre operator), then append it:

```sh
tkit record add infrastructure -f zone.json --by legal@acme.example
```

Do the same for `measures`, the protections you apply against foreign governmental access.
Together these two record types produce your Data Act Art. 28 page.

## 4. Record a request

```sh
tkit record template gov_request > request.json
# edit: authority, legal basis, outcome, whether the customer was notified
tkit record add gov_request -f request.json
```

Case references and notes go in the `private` object. They are hashed into the ledger but never
appear in anything published.

When a request's outcome changes, append an amendment instead of editing:

```sh
tkit record add gov_request -f request-final.json --amends 5
```

## 5. Check the ledger

```sh
tkit ledger verify
```

This recomputes every hash and confirms the chain is unbroken. Try editing a file under
`ledger/records/` by hand and run it again: it names the record that changed.

## 6. Publish

```sh
tkit publish --period 2026
```

The `public/` directory now holds the report, the Data Act page, the DSA CSVs, the subprocessor
feed and a signed `manifest.json`. Look at it in a browser:

```sh
tkit serve        # http://localhost:8080
```

## 7. Verify like an outsider would

```sh
tkit verify --pubkey transparencykit.pub --chain file://$PWD/public
```

Anyone with your public key can run the same check against your published URL. See
[Verifying a publication](verifying.md).

## Try the full demo

The repository ships a fictional provider with 117 records:

```sh
git clone https://github.com/transparencykit/transparencykit
cd transparencykit
tools/demo.sh --serve
```
