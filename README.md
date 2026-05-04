# pour

> **This README documents v0.2.0. It will not be accurate for earlier or later versions.**

> **This project is under active development.** The API, config schema, and CLI flags are not stable until v1.0.0.
> Pre-1.0 minor releases may add new config keys but aim not to remove or rename existing ones. v1.0.0 is planned to
> follow v0.7.0 once the feature set is complete and the interfaces have been battle-tested — hopefully without too long
> a gap, but no hard timeline is promised.

A pure-Go, multi-chain Cosmos faucet. Single static binary, no Node, no CGO, no shelling out to chain CLIs. Builds and
broadcasts transactions via raw protobuf over gRPC — no cosmos-sdk import required. Chains are sourced from the
[cosmos/chain-registry](https://github.com/cosmos/chain-registry) and cached locally; standalone mode is available for
chains not in the public registry.

## Install

```sh
go install github.com/ny4rl4th0t3p/pour/cmd/pour@latest
```

Or build from source:

```sh
make build
./pour version
```

## Quick start

```sh
# Generate a fresh mnemonic for the faucet wallet
pour keys generate

# Configure your chains
cp chains.yml.example chains.yml
$EDITOR chains.yml

# Run
export POUR_MNEMONIC="word word word ..."
pour serve
```

Fund the address derived from `POUR_MNEMONIC` on each chain before starting.

On first start, an admin token is auto-generated and written to `.pour-admin-token`. Set `POUR_ADMIN_TOKEN` to supply
your own.

## Configuration

`chains.yml` (see `chains.yml.example`):

```yaml
abuse:
  ip_rate_limit:
    requests_per_window: 10
    window: 1h

# Optional — defaults shown.
registry:
  base_url: ""              # defaults to cosmos/chain-registry on GitHub
  refresh_interval: "6h"   # how often to re-fetch chain metadata

# Optional — restricts /admin/* to localhost by default.
admin:
  allowed_cidrs:
    - "127.0.0.1/32"

chains:
  # Registry chain: metadata (bech32, slip44, endpoints, fee tokens) is pulled
  # from the public registry. Only drip and optional overrides are needed.
  - chain_id: osmosis-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"

  # Standalone chain: not in the public registry — all fields must be provided.
  - chain_id: mynet-1
    standalone: true
    enabled: true
    bech32_prefix: mynet
    slip44: 118
    endpoints:
      grpc:
        - grpc.mynet.example.com:9090
    fee_tokens:
      - denom: umynet
        low_gas_price: "0.001"
        average_gas_price: "0.025"
    drip:
      anonymous: "1000000umynet"
```

## Environment variables

| Variable           | Default              | Description                                              |
|--------------------|----------------------|----------------------------------------------------------|
| `POUR_MNEMONIC`    | —                    | **Required.** BIP39 mnemonic for the faucet wallet.      |
| `POUR_ADMIN_TOKEN` | *(auto-generated)*   | Admin API bearer token. Auto-written to `.pour-admin-token` on first start. |
| `POUR_CONFIG`      | `chains.yml`         | Path to the chains config file.                          |
| `POUR_LISTEN`      | `:8080`              | Address to listen on.                                    |
| `POUR_DB_PATH`     | `pour.db`            | Path to the SQLite database.                             |
| `POUR_LOG_LEVEL`   | `info`               | Log level: `debug`, `info`, `warn`, `error`.             |
| `POUR_METRICS`     | `false`              | Enable Prometheus metrics at `/metrics`.                 |
| `POUR_NO_UI`       | `false`              | Disable the embedded web UI.                             |
| `POUR_ADMIN_URL`   | `http://localhost:8080` | Base URL used by `pour chains` CLI subcommands.       |

## HTTP API

```
POST /v1/pour                  drip tokens to an address
GET  /v1/chains                list enabled chains and drip config
GET  /v1/chains/{chain_id}     detail for a single chain
GET  /v1/info                  version and feature flags
GET  /health                   liveness probe — {"status":"ok"}
GET  /metrics                  Prometheus metrics (if POUR_METRICS=true)

GET  /admin/registry/snapshot  full resolved registry view (admin auth required)
GET  /admin/registry/pending   pending registry changes awaiting acceptance
POST /admin/registry/accept    accept a pending change for a specific field
POST /admin/registry/refresh   trigger an immediate registry re-fetch
POST /admin/reload             hot-reload chains.yml without restart
```

**Drip request:**

```sh
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1","address":"osmo1..."}'
```

**Response:**

```json
{
  "drip_id": 1,
  "status": "confirmed",
  "amount": "1000000uosmo",
  "tx_hash": "ABC123..."
}
```

**Admin requests** require a `Bearer` token:

```sh
TOKEN=$(cat .pour-admin-token)
curl -X POST http://localhost:8080/admin/registry/refresh \
  -H "Authorization: Bearer $TOKEN"
```

## CLI

```
pour serve                     start the faucet server
pour keys generate             generate a new BIP39 mnemonic
pour version                   print version, commit, and build date

pour chains list               list chains from the running daemon
pour chains validate           validate a chains.yml file offline
pour chains diff               show overrides that differ from the live registry
pour chains pending            list pending registry changes
pour chains accept             accept a pending change for a field
pour chains pin                emit config snippet to pin a field to its current registry value
pour chains refresh            trigger an immediate registry re-fetch
```

## Roadmap

- [x] **v0.1.0** — single-chain drip, embedded UI, IP rate limiting
- [x] **v0.2.0** — chain registry integration, multi-chain runtime, admin API
- [ ] **v0.3.0** — batch window, multiple distributor wallets
- [ ] **v0.4.0** — PoW challenge, API keys, signed-wallet tier
- [ ] **v0.5.0** — IBC plumbing
- [ ] **v0.6.0** — IBC drips
- [ ] **v0.7.0** — devnet tooling and local testing helpers
- [ ] **v1.0.0** — stable: API and config schema frozen under semver guarantees

## License

Apache-2.0 — see [LICENSE](LICENSE).