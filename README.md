# pour

A pure-Go, single-chain Cosmos faucet. Single static binary, no Node, no CGO, no shelling out to chain CLIs. Builds and
broadcasts transactions via raw protobuf over gRPC — no cosmos-sdk import required.

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

# Configure your chain
cp chains.yml.example chains.yml
$EDITOR chains.yml

# Run
export POUR_MNEMONIC="word word word ..."
pour serve
```

Fund the address derived from `POUR_MNEMONIC` on your chain before starting.

## Configuration

`chains.yml` (see `chains.yml.example`):

```yaml
abuse:
  ip_rate_limit:
    requests_per_window: 10
    window: 1h

chains:
  - chain_id: osmosis-1
    enabled: true
    bech32_prefix: osmo
    slip44: 118
    endpoints:
      grpc:
        - grpc.osmosis.zone:443
    fee_tokens:
      - denom: uosmo
        average_gas_price: "0.025"
        low_gas_price: "0.01"
    drip:
      anonymous: "1000000uosmo"           # required
      max_per_address_per_day: "50000000uosmo"  # required
```

## Environment variables

| Variable         | Default      | Description                                         |
|------------------|--------------|-----------------------------------------------------|
| `POUR_MNEMONIC`  | —            | **Required.** BIP39 mnemonic for the faucet wallet. |
| `POUR_CONFIG`    | `chains.yml` | Path to the chains config file.                     |
| `POUR_LISTEN`    | `:8080`      | Address to listen on.                               |
| `POUR_DB_PATH`   | `pour.db`    | Path to the SQLite database.                        |
| `POUR_LOG_LEVEL` | `info`       | Log level: `debug`, `info`, `warn`, `error`.        |
| `POUR_METRICS`   | `false`      | Enable Prometheus metrics at `/metrics`.            |

## HTTP API

```
POST /v1/pour          drip tokens to an address
GET  /v1/chains        list enabled chains and drip config
GET  /v1/info          version and feature flags
GET  /health           liveness probe — {"status":"ok"}
GET  /metrics          Prometheus metrics (if POUR_METRICS=true)
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

## CLI

```
pour serve              start the faucet server
pour keys generate      generate a new BIP39 mnemonic
pour version            print version, commit, and build date
```

## Roadmap

- [x] **v0.1.0** — single-chain drip, embedded UI, IP rate limiting
- [ ] **v0.2.0** — chain registry integration, multi-chain runtime
- [ ] **v0.3.0** — batch window, multiple distributor wallets
- [ ] **v0.4.0** — PoW challenge, API keys, signed-wallet tier
- [ ] **v0.5.0** — IBC plumbing
- [ ] **v0.6.0** — IBC drips
- [ ] **v0.7.0** — devnet tooling and local testing helpers

## License

Apache-2.0 — see [LICENSE](LICENSE).