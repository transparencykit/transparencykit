# Storage requirements

TransparencyKit needs two buckets in S3-compatible object storage: a private **ledger** bucket
and a public **reports** bucket. This page lists what each must support. Check it before you
pick a provider or create the buckets, because object lock cannot be added later.

For local evaluation you need none of this: `file://` directories work out of the box (see
[Getting started](getting-started.md)).

## At a glance

| | Ledger bucket (private) | Reports bucket (public) |
|---|---|---|
| Object lock | **Required for production**, enabled at bucket creation | Not wanted |
| Versioning | On (object lock turns it on) | Optional |
| Default retention | GOVERNANCE or COMPLIANCE, e.g. 10 years | None |
| Public access | None | Public read on objects |
| Lifecycle / expiration rules | **None**: they must never delete records | Optional |
| Writers | One (unless conditional writes are confirmed) | One |
| API key | Get, put, list on this bucket only; no delete | Get, put, list on this bucket only |

## S3 API features used

Your provider must support these. Most S3-compatible services do; Exoscale SOS does.

| Feature | Used for | If missing |
|---|---|---|
| `PutObject`, `GetObject`, `ListObjectsV2` with continuation tokens | Everything | Not usable |
| AWS Signature Version 4 | Authentication | Not usable |
| Path-style addressing (`https://endpoint/bucket/key`) | Default for `sos://` and `s3://` URLs | Add `&path_style=false` to an `s3://` URL for virtual-hosted style |
| `Content-MD5` on `PutObject` | Integrity of every upload; S3 requires it when object lock applies | Uploads rejected |
| Object lock and retention headers (`x-amz-object-lock-mode`, `x-amz-object-lock-retain-until-date`) | Making ledger records undeletable | Tamper-*evident* only: edits are detected by `ledger verify`, but not prevented |
| `x-amz-acl: public-read` on `PutObject` | Making published files readable | Make the reports bucket public with a bucket policy instead |
| Conditional writes (`If-None-Match: *`) | Safe concurrent appends; never overwriting a published version | Run a single writer; see below |
| Static website hosting | **Not needed** | Use direct object URLs or `tkit serve` |
| Event notifications, bucket logging | **Not needed** | — |

## Ledger bucket

**Create it with object lock enabled.** On Exoscale SOS, as on AWS S3, object lock can only be
switched on when the bucket is created, and it turns on versioning. With the MinIO client:

```sh
mc alias set sos https://sos-ch-gva-2.exo.io "$EXOSCALE_API_KEY" "$EXOSCALE_API_SECRET"
mc mb --with-lock sos/acme-transparency-ledger
mc retention set --default GOVERNANCE 3650d sos/acme-transparency-ledger
```

Or with the AWS CLI against any S3-compatible endpoint:

```sh
aws s3api create-bucket --bucket acme-transparency-ledger \
  --object-lock-enabled-for-bucket --endpoint-url https://sos-ch-gva-2.exo.io
aws s3api put-object-lock-configuration --bucket acme-transparency-ledger \
  --endpoint-url https://sos-ch-gva-2.exo.io \
  --object-lock-configuration '{"ObjectLockEnabled":"Enabled","Rule":{"DefaultRetention":{"Mode":"GOVERNANCE","Years":10}}}'
```

**Choose the retention mode deliberately.**

- **GOVERNANCE**: holders of a special permission can still shorten retention or delete a
  locked object. Start here.
- **COMPLIANCE**: nobody, including the account owner, can delete or shorten retention until it
  expires. Switch only after a dry run, because a mistake cannot be undone.

You can also have TransparencyKit set retention on each record it writes, in addition to the
bucket default:

```json
"ledger_retention": { "mode": "GOVERNANCE", "days": 3650 }
```

**Never add lifecycle or expiration rules to the ledger bucket.** A deleted record breaks the
hash chain, and `ledger verify` will refuse every later publication.

**Use a dedicated API key.** Exoscale IAM can restrict a key to a single bucket. Where your
provider's IAM allows per-operation rules, give the recording key read, write and list on the
ledger bucket and no delete permission. Keep it separate from the publishing key.

**Run one writer.** Each record is written with `If-None-Match: *`, so two people appending at
the same moment cannot both claim the same sequence number, provided the storage honours
conditional writes. Exoscale does not document whether SOS does. Until you have confirmed it
(the opt-in `TestSOSIntegration` reports it), let one person or one CI job append records. If two
writers ever race on a backend without conditional writes, versioning keeps the overwritten
record and `ledger verify` reports the break.

## Reports bucket

- **No object lock.** TransparencyKit rewrites the convenience copies (`latest/`,
  `latest.json`, `index.html`, `transparencykit.pub`) on every publication. Versioned report
  folders (`reports/<period>/v<N>/`) are never rewritten.
- **Public read.** Files are uploaded with a `public-read` ACL. If your provider ignores ACLs,
  make the bucket readable with a bucket policy instead.
- **No website hosting needed.** Exoscale SOS has none. Link to full object URLs such as
  `https://acme-transparency.sos-ch-gva-2.exo.io/latest/index.html`, put a CDN in front, or run
  `tkit serve`. Folder URLs ending in `/` do not resolve to `index.html` on plain object storage.

## API keys

Use one IAM key per bucket. `publish` needs both at once (it reads the ledger and writes the
reports), so each store reads its own variables:

| Store | Access key | Secret |
|---|---|---|
| Ledger | `TRANSPARENCYKIT_LEDGER_ACCESS_KEY` | `TRANSPARENCYKIT_LEDGER_SECRET_KEY` |
| Reports | `TRANSPARENCYKIT_PUBLIC_ACCESS_KEY` | `TRANSPARENCYKIT_PUBLIC_SECRET_KEY` |

A store without its own pair uses `EXOSCALE_API_KEY` / `EXOSCALE_API_SECRET`, then
`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`. Keys are never read from `transparencykit.json`.

Interrupted uploads can leave a report folder without `manifest.sig`. The next `publish` skips it
and logs it; the reports key may delete it, but nothing requires that.

## Costs on Exoscale SOS

Every record is one small object. SOS bills each object as at least 128 KB, so 1,000 records
count as about 128 MB of storage. Each publication adds about 16 small files.

Exoscale SOS keeps at most 1,000 versions of one object. TransparencyKit never rewrites anything
in the ledger bucket, so this limit only touches the convenience copies in the reports bucket,
and only if you turn on versioning there and publish more than 1,000 times.

## Checklist

1. Ledger bucket created **with object lock**, default retention set, no lifecycle rules.
2. Reports bucket created without lock; objects publicly readable.
3. Two API keys, each restricted to one bucket, neither allowed to delete, exported as
   `TRANSPARENCYKIT_LEDGER_ACCESS_KEY` / `_SECRET_KEY` and `TRANSPARENCYKIT_PUBLIC_ACCESS_KEY` /
   `_SECRET_KEY` (see [API keys](#api-keys)).
4. `ledger` and `public` URLs set in `transparencykit.json`, for example
   `sos://acme-transparency-ledger/?zone=ch-gva-2`.
5. `tkit record add …` then `tkit ledger verify` succeeds.
6. `tkit publish --period <year>` then `tkit verify --pubkey transparencykit.pub <public URL>`
   succeeds from a machine outside your network.

Sources: Exoscale documentation on
[versioning and object lock](https://community.exoscale.com/product/storage/object-storage/how-to/versioning/),
[limits and unsupported S3 features](https://community.exoscale.com/product/storage/object-storage/service-boundaries/limits-and-quotas/)
and the [Object Storage overview](https://community.exoscale.com/product/storage/object-storage/overview/).
