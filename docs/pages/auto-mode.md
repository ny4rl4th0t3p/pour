# Auto mode (devnets)

!!! tip "Uncommon capability"
Most Cosmos faucets require a fully-written `chains.yml` even for a local devnet — you have to know
the chain ID, bech32 prefix, denom, gas prices, and gRPC endpoint before the faucet will start.
Pour's auto mode eliminates all of that: point it at the chain's home directory and it figures
everything out from `genesis.json`.

Auto mode lets pour self-configure from a local chain's genesis file. No manual `chains.yml` entry
is needed — pour reads the genesis, derives the chain ID, bech32 prefix, and native denom, then
generates or reloads a faucet mnemonic and starts serving.

## Basic usage

```sh
pour serve --auto --home ~/.simapp
```

`--home` must point to the chain's home directory. pour reads
`<home>/config/genesis.json` on startup.

### With self-funding

```sh
pour serve --auto --home ~/.simapp --fund-mnemonic "word word word ..."
```

When `--fund-mnemonic` is provided, pour derives the funded account's address and sends tokens to
the faucet address before opening the HTTP server. This removes the manual step of pre-funding the
faucet wallet.

The env var `POUR_FUND_MNEMONIC` is equivalent to `--fund-mnemonic`.

## Faucet mnemonic lifecycle

On first run, pour generates a random BIP39 mnemonic and writes it to `~/.pour/auto-mnemonic`
(mode `0600`). The derived address is logged — fund it before pour will accept drip requests
(unless `--fund-mnemonic` is set).

On subsequent runs the same mnemonic is reloaded from `~/.pour/auto-mnemonic`.
Set `POUR_MNEMONIC` to override and use a different key.

## Flag reference

These flags are only active when `--auto` is set.

| Flag                 | Default                  | Description                                                                            |
|----------------------|--------------------------|----------------------------------------------------------------------------------------|
| `--home`             | *(required)*             | Chain home directory. Genesis is read from `<home>/config/genesis.json`.               |
| `--grpc`             | `localhost:9090`         | gRPC endpoint of the running chain.                                                    |
| `--rpc`              | `http://localhost:26657` | Tendermint/CometBFT RPC endpoint (used for hot-reload detection).                      |
| `--drip`             | `1000000<denom>`         | Drip amount per request (e.g. `500000uatom`). Defaults to 1 token at 6 decimal places. |
| `--fund-mnemonic`    | —                        | Mnemonic of a funded genesis account. When set, pour self-funds on startup.            |
| `POUR_FUND_MNEMONIC` | —                        | Environment variable equivalent of `--fund-mnemonic`.                                  |

## LCD / REST-only chains

If the chain node exposes only a REST (LCD) endpoint and no gRPC, pass `--rest` instead of `--grpc`:

```sh
pour serve --auto --home ~/.simapp --grpc "" --rest http://localhost:1317
```

See [Transports](transports.md) for details on gRPC vs REST transport selection.

## Hot reload (devnet reset detection)

Pour polls the RPC endpoint (`--rpc`) every 5 seconds for the latest block height. If the height
drops below the last observed value (indicating a `unsafe-reset-all` or equivalent chain reset),
pour:

1. Closes all existing gRPC connections
2. Reconnects to the chain from height 0
3. Resumes serving drip requests

This means you can reset a local chain without restarting the faucet process.

!!! note
Hot reload requires the RPC endpoint to be reachable. It has no effect in production
deployments where block height regressions do not occur.

## Example: ignite chain serve

```sh
# Terminal 1
ignite chain serve

# Terminal 2 — in your chain's directory
pour serve --auto --home ~/.mylocalchain/
```

## Example: simd start

```sh
# Terminal 1
simd start --home ~/.simapp

# Terminal 2 — pass the mnemonic you used when initialising the chain
pour serve --auto --home ~/.simapp --fund-mnemonic "word word word ..."
```

## What gets auto-configured

| Field                          | Source                                                                                 |
|--------------------------------|----------------------------------------------------------------------------------------|
| `chain_id`                     | `genesis.json → chain_id`                                                              |
| `bech32_prefix`                | Inferred from the HRP of the first address in `genesis.json → app_state.bank.balances` |
| `native_denom`                 | `genesis.json → app_state.staking.params.bond_denom`                                   |
| `slip44`                       | Always `118` (standard Cosmos coin type)                                               |
| `drip.anonymous`               | `--drip` flag or `1000000<denom>`                                                      |
| `drip.max_per_address_per_day` | `10 × drip.anonymous`                                                                  |
| `batch_window`                 | `"0s"` (synchronous — recommended for single-node devnets)                             |
| `endpoints.grpc`               | `--grpc` flag                                                                          |
| `endpoints.rest`               | `--rest` flag (if set)                                                                 |