# Installation & quickstart

## Install

```sh
go install github.com/ny4rl4th0t3p/pour/cmd/pour@latest
```

Or build from source:

```sh
git clone https://github.com/ny4rl4th0t3p/pour
cd pour
make build
./pour version
```

## Devnet quickstart (auto mode)

If you're running a local chain with `ignite chain serve`, `simd start`, or similar, pour can
self-configure from the genesis file — no `chains.yml` needed:

```sh
pour serve --auto --home ~/.simapp
```

That's it. Pour reads `genesis.json`, derives the chain ID, bech32 prefix, and native denom,
generates a faucet wallet, and starts serving at `http://localhost:8080`. Fund the printed address
(or pass `--fund-mnemonic` to self-fund from a genesis account), and your devnet has a faucet.

See [Auto mode](auto-mode.md) for the full flag reference and hot-reload behaviour.

## Production / testnet quickstart

### 1. Generate a faucet wallet

```sh
pour keys generate
```

This prints a fresh BIP39 mnemonic. Derive the corresponding address for each chain you intend to
serve using your wallet app or the chain CLI, then fund it before starting the faucet.

### 2. Configure chains

```sh
cp chains.yml.example chains.yml
$EDITOR chains.yml
```

Minimum viable config for a public chain:

```yaml
chains:
  - chain_id: osmosis-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"
      max_per_address_per_day: "50000000uosmo"
```

Chain metadata (bech32 prefix, endpoints, fee tokens) is pulled automatically from the
[cosmos/chain-registry](https://github.com/cosmos/chain-registry). See
[chains.yml reference](configuration.md) for the full field list.

### 3. Run

```sh
export POUR_MNEMONIC="word word word ..."
pour serve
```

The faucet is now available at `http://localhost:8080`.

## Environment variables

| Variable           | Default                 | Description                                                              |
|--------------------|-------------------------|--------------------------------------------------------------------------|
| `POUR_MNEMONIC`    | —                       | **Required.** BIP39 mnemonic for the faucet wallet.                      |
| `POUR_ADMIN_TOKEN` | *(auto-generated)*      | Admin API bearer token. Used only if no `.pour-admin-token` file exists. |
| `POUR_CONFIG`      | `chains.yml`            | Path to the chains config file.                                          |
| `POUR_LISTEN`      | `:8080`                 | Address to listen on.                                                    |
| `POUR_DB_PATH`     | `pour.db`               | Path to the SQLite database (drip records).                              |
| `POUR_LOG_LEVEL`   | `info`                  | Log level: `debug`, `info`, `warn`, `error`.                             |
| `POUR_METRICS`     | `false`                 | Enable Prometheus metrics at `/metrics`.                                 |
| `POUR_NO_UI`       | `false`                 | Disable the embedded web UI.                                             |
| `POUR_ADMIN_URL`   | `http://localhost:8080` | Base URL used by `pour chains` CLI subcommands.                          |

## Admin token

The admin token resolves in priority order:

1. `.pour-admin-token` file in the working directory
2. `POUR_ADMIN_TOKEN` environment variable
3. Auto-generated at startup, written to `.pour-admin-token`

On a fresh start with neither set, the generated token is logged and written to `.pour-admin-token`.
Once the file exists it takes precedence over the env var, so rotations survive restarts. To revert
to an env-var-managed token, delete the file.

```sh
cat .pour-admin-token   # read the current token
```

## CLI reference

```
pour serve                     start the faucet server
pour keys generate             generate a new BIP39 mnemonic
pour version                   print version, commit, and build date

pour chains list               list chains from the running daemon
pour chains validate           validate a chains.yml file offline
pour chains diff               show overrides that differ from the live registry
pour chains pending            list pending registry changes
pour chains accept             accept a pending change for a field
pour chains pin                emit config snippet to pin a field to its current value
pour chains refresh            trigger an immediate registry re-fetch
```

## Sending your first drip

```sh
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1","address":"osmo1..."}'
```

**Async response** (default, `batch_window > 0`):

```json
{
  "drip_id": 42,
  "status": "queued",
  "amount": "1000000uosmo",
  "mechanism": "anonymous"
}
```

**Sync response** (`batch_window: "0"`):

```json
{
  "drip_id": 1,
  "status": "confirmed",
  "amount": "1000000uosmo",
  "mechanism": "anonymous",
  "tx_hash": "ABC123..."
}
```