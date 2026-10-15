# Documentation

TransparencyKit keeps a tamper-evident ledger of the events a cloud provider is expected to be
open about, and publishes signed reports from it that anyone can verify.

It is one static binary (`transparencykit`, or its short name `tkit`). It stores everything in
S3-compatible object storage: Exoscale SOS, any other S3 service, or a local directory while you
evaluate it.

## Start here

| If you want to… | Read |
|---|---|
| Try it in five minutes on your laptop | [Getting started](getting-started.md) |
| Look up a command or flag | [CLI reference](cli.md) |
| Check a provider's published report | [Verifying a publication](verifying.md) |
| Run it in production on Exoscale SOS | [Operations](OPERATIONS.md) |
| Understand the formats and guarantees exactly | [Specification](SPEC.md) |
| See what the test suite proves | [Testing](TESTING.md) |

## What it records

| Record type | Examples |
|---|---|
| `gov_request` | Police or court orders, CLOUD Act requests, DSA Art. 9 and Art. 10 orders |
| `abuse_notice` | DSA Art. 16 notices, trusted-flagger notices, own-initiative moderation |
| `subprocessor_change` | Adding, updating or removing a subprocessor |
| `infrastructure` | The country, legal entity and operator behind each zone |
| `measures` | Technical, organisational and contractual protections against foreign access |
| `retraction` | Excluding a record entered by mistake (the original stays in the ledger) |

## What it publishes

| Output | For |
|---|---|
| Transparency report (HTML and JSON) | Customers, journalists, researchers |
| EU Data Act Art. 28 page | The page your contracts must reference |
| DSA Art. 15 CSVs | The annual report hosting providers owe under the DSA |
| Subprocessor list and Atom feed | Customers who must be told about changes |
| Signed manifest | Anyone who wants to check that nothing was altered |

TransparencyKit is not legal advice. Have counsel review categorisations and map the DSA files
to the Commission's current template before filing.
