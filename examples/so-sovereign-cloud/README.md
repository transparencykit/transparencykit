# Example: So Sovereign Cloud SA

A fictional Swiss cloud provider with three zones, a handful of subprocessors and a year of
government requests and abuse notices (117 records). The website publishes its 2026 report at
<https://transparencykit.dev/example/>, rebuilt and re-signed on every
deployment.

| Zone | Location | Data-centre operator |
|---|---|---|
| `ch-lmn-1` | Geneva, CH | Lac Léman Datacentre SA |
| `ch-uet-1` | Zurich, CH | Uetliberg Rechenzentrum AG |
| `de-mai-1` | Frankfurt, DE | Mainhattan Colocation GmbH |

All names, domains (`so-sovereign.example`) and figures are invented.

## Verify the published example

```sh
tkit verify --pubkey examples/so-sovereign-cloud/transparencykit.pub --chain \
  https://transparencykit.dev/example
```

## Rebuild it locally

```sh
tools/demo.sh --serve      # http://localhost:8080
```

## Files

| File | Purpose |
|---|---|
| `records.json` | The 117 records, imported with `tkit record import` |
| `transparencykit.json` | Provider configuration |
| `transparencykit.pub` | Public key that signs the published example |
| `transparencykit.key` | **Demo-only private key, committed on purpose so anyone can rebuild the example.** Never commit a real signing key. |
