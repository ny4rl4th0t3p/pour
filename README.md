# pour

> **This project is under active development.** The API, config schema, and CLI flags are not stable until v1.0.0.
> Pre-1.0 minor releases may add new config keys but aim not to remove or rename existing ones. v1.0.0 is planned once
> the feature set is complete and the interfaces have been battle-tested — hopefully without too long a gap, but no hard
> timeline is promised.

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

**Production / testnet**

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

**Local devnet (`ignite chain serve`, `simd start`, etc.)**

```sh
pour serve --auto --home ~/.simapp
```

Pour reads the genesis file from `--home`, infers the chain ID, bech32 prefix, and native denom
automatically. A faucet mnemonic is generated on first run and persisted to `~/.pour/auto-mnemonic`
for subsequent restarts. Fund the printed faucet address before requests are accepted, or pass
`--fund-mnemonic` to self-fund from a genesis account:

```sh
pour serve --auto --home ~/.simapp --fund-mnemonic "word word word ..."
```

See [auto mode flags](#auto-mode-flags) for the full flag reference.

The admin token resolves in order: `.pour-admin-token` file → `POUR_ADMIN_TOKEN` env var → auto-generate and write to
`.pour-admin-token`. On a fresh start with neither set, the generated token is logged and written to `.pour-admin-token`
— read it with `cat .pour-admin-token`. Once the file exists it takes precedence over the env var, so rotations survive
restarts without the env var winning back. To revert to an env-var-managed token, delete the file.

## Configuration

`chains.yml` (see `chains.yml.example`):

```yaml
abuse:
  # Proof-of-work gate (Altcha). All mechanisms default to false — enable what you need.
  pow:
    enabled: true
    difficulty: medium   # easy | medium | hard | <positive integer>

  # API key authentication.
  api_keys:
    enabled: true

  # Signed-challenge authentication (Cosmos wallet signature).
  signature_challenge:
    enabled: false
    require_predicate: none       # none | has_balance
    # predicate_chain_id: ""      # chain to query for the predicate; defaults to the chain being dripped
    # predicate_min_amount: ""    # required when require_predicate is has_balance (e.g. "1000000uatom")

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
      anonymous: "1000000uosmo"           # amount for anonymous and PoW requests
      signed: "5000000uosmo"              # amount for signed-wallet requests (optional)
      max_per_address_per_day: "50000000uosmo"

    # Concurrency — optional; defaults shown.
    distributors: 1               # signing accounts (key indices 1..N); holder is index 0
    batch_window: "5s"            # coalesce requests into MsgMultiSend; "0" = synchronous
    max_recipients_per_batch: 25  # outputs per MsgMultiSend
    # max_queue_depth: 500          # per-distributor queue cap; 0 = unlimited (not recommended)
    # refill_threshold: ""          # min distributor balance before holder refills it
    #                               # default: drip.anonymous × distributors × 10

    # IBC — optional.
    # ibc:
    #   timeout: 10m               # MsgTransfer timeout; default 10m; increase for slow relayers

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
      max_per_address_per_day: "10000000umynet"
```

## Abuse prevention

Pour evaluates each request through a priority-ordered gate. The first mechanism that matches
determines how much the requester receives:

| Priority | Mechanism     | Drip amount                                             | Requirement                                        |
|----------|---------------|---------------------------------------------------------|----------------------------------------------------|
| 1        | **API key**   | Per-key override, or `drip.anonymous`                   | `Authorization: Bearer pour_key_…`                 |
| 2        | **Signed**    | `drip.signed` (falls back to `drip.anonymous` if unset) | Cosmos wallet signature over a server-issued nonce |
| 3        | **PoW**       | `drip.anonymous`                                        | Valid Altcha solution from `GET /v1/pow/challenge` |
| 4        | **Anonymous** | `drip.anonymous`                                        | Nothing — if all mechanisms are disabled           |

The per-address daily cap (`drip.max_per_address_per_day`) is always enforced against the resolved
drip amount, regardless of mechanism. The same key is recognised across all chain prefixes (cosmos1…,
osmo1…, etc.) that share the same underlying public key, so switching prefixes cannot bypass the cap.

### API keys

API keys are intended for programmatic access — CI pipelines, scripts, and dev tools that call
the pour API directly. They are not surfaced in the web UI.

Issued and managed via the admin API. Each key carries an optional per-chain drip override and an
optional per-hour request ceiling. Chain scope restricts which chains the key can drip from.

```sh
# Issue a key
curl -X POST http://localhost:8080/admin/api-keys \
  -H "Authorization: Bearer $(cat .pour-admin-token)" \
  -H 'Content-Type: application/json' \
  -d '{"label":"ci-bot","chain_scope":["osmosis-1"],"per_chain_drips":{"osmosis-1":"3000000uosmo"}}'

# Use the returned secret in pour requests
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer pour_key_…' \
  -d '{"chain_id":"osmosis-1","address":"osmo1…"}'
```

### Signed wallet

The client fetches a one-time nonce from `GET /v1/sign/nonce`, signs it using the Cosmos ADR-036
arbitrary-message format (supported by Keplr and most Cosmos wallets), then includes the signature
in the pour request. The server verifies that the signature is valid and that the public key
corresponds to the claimed address.

An optional on-chain predicate can be required before the higher drip amount is granted:

| `require_predicate` | What is checked                                                   |
|---------------------|-------------------------------------------------------------------|
| `none` *(default)*  | Signature only — no chain query                                   |
| `has_balance`       | Signer's balance on `predicate_chain_id` ≥ `predicate_min_amount` |

`predicate_chain_id` defaults to the chain being dripped; set it to e.g. `cosmoshub-4` to require
holders of ATOM regardless of which testnet they are requesting from. Chains used only as predicate
sources should have `enabled: false` — enabling them would open a drip endpoint for a chain the
faucet has no funds on.

### Proof-of-work

The Altcha widget embedded in the UI handles PoW automatically. For direct API access:

```sh
# Fetch a challenge
CHALLENGE=$(curl -s http://localhost:8080/v1/pow/challenge | jq -r .challenge)

# Solve it client-side (Altcha JS library) and include the solution in the pour request
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -d "{\"chain_id\":\"osmosis-1\",\"address\":\"osmo1…\",\"pow\":{\"challenge\":\"$CHALLENGE\",\"solution\":\"…\"}}"
```

## Environment variables

| Variable           | Default                 | Description                                                                                        |
|--------------------|-------------------------|----------------------------------------------------------------------------------------------------|
| `POUR_MNEMONIC`    | —                       | **Required.** BIP39 mnemonic for the faucet wallet.                                                |
| `POUR_ADMIN_TOKEN` | *(auto-generated)*      | Admin API bearer token. Used only if no `.pour-admin-token` file exists; see token priority above. |
| `POUR_CONFIG`      | `chains.yml`            | Path to the chains config file.                                                                    |
| `POUR_LISTEN`      | `:8080`                 | Address to listen on.                                                                              |
| `POUR_DB_PATH`     | `pour.db`               | Path to the SQLite database.                                                                       |
| `POUR_LOG_LEVEL`   | `info`                  | Log level: `debug`, `info`, `warn`, `error`.                                                       |
| `POUR_METRICS`     | `false`                 | Enable Prometheus metrics at `/metrics`.                                                           |
| `POUR_NO_UI`       | `false`                 | Disable the embedded web UI.                                                                       |
| `POUR_ADMIN_URL`   | `http://localhost:8080` | Base URL used by `pour chains` CLI subcommands.                                                    |

### Auto mode flags

These flags are only active when `--auto` is set. They do not affect normal `pour serve` operation.

| Flag                 | Default                  | Description                                                                             |
|----------------------|--------------------------|-----------------------------------------------------------------------------------------|
| `--home`             | *(required)*             | Chain home directory. Genesis file is read from `<home>/config/genesis.json`.           |
| `--grpc`             | `localhost:9090`         | gRPC endpoint of the running chain.                                                     |
| `--rpc`              | `http://localhost:26657` | Tendermint RPC endpoint (used for devnet hot-reload detection).                         |
| `--drip`             | `1000000<denom>`         | Drip amount per request (e.g. `500000uatom`). Defaults to 1 token at 6 decimal places.  |
| `--fund-mnemonic`    | —                        | Mnemonic of a funded genesis account. When set, pour self-funds its address on startup. |
| `POUR_FUND_MNEMONIC` | —                        | Env-var equivalent of `--fund-mnemonic`.                                                |

The faucet mnemonic is auto-generated on first run and saved to `~/.pour/auto-mnemonic` (mode
`0600`). On subsequent runs the same mnemonic is reloaded. Set `POUR_MNEMONIC` to override.

## HTTP API

```
POST /v1/pour                              drip tokens to an address
GET  /v1/pow/challenge                     fetch an Altcha PoW challenge (when pow.enabled)
GET  /v1/sign/nonce                        fetch a one-time signing nonce (when signature_challenge.enabled)
GET  /v1/chains                            list enabled chains and drip config
GET  /v1/chains/{chain_id}                 detail for a single chain
GET  /v1/info                              version, abuse flags, and registry status
GET  /health                               liveness probe — {"status":"ok"}
GET  /metrics                              Prometheus metrics (if POUR_METRICS=true)

GET  /admin/registry/snapshot              full resolved registry view (admin auth required)
GET  /admin/registry/pending               pending registry changes awaiting acceptance
POST /admin/registry/accept                accept a pending change for a specific field
POST /admin/registry/refresh               trigger an immediate registry re-fetch
POST /admin/reload                         hot-reload chains.yml without restart

GET  /admin/distributors/{chain}           distributor balances, queue depths, and status
POST /admin/distributors/{chain}/refill    trigger immediate refill from holder

GET  /admin/chains/{chain}/gas-cache       current learned gas parameters
POST /admin/chains/{chain}/gas-cache/reset clear learned gas; forces cold-start estimation

GET  /admin/chains/{chain}/status          chain operational state (suspended, multisend_disabled, …)
POST /admin/chains/{chain}/resume          clear suspension; chain resumes accepting pours

POST /admin/api-keys                       issue a new API key (secret returned once)
GET  /admin/api-keys                       list active keys (no secrets)
DELETE /admin/api-keys/{id}               revoke a key immediately
POST /admin/api-keys/rotate-admin          rotate the admin bearer token; old token rejected immediately
```

**`GET /v1/chains/{chain_id}`** includes an `ibc_channels` array — one entry per live ICS-20
channel the chain has with other configured chains:

```json
{
  "chain_id": "osmosis-1",
  "ibc_channels": [
    {
      "peer_chain_name": "cosmoshub",
      "channel_id": "channel-0",
      "peer_channel_id": "channel-141",
      "port_id": "transfer",
      "status": "live",
      "preferred": true
    }
  ]
}
```

**`GET /v1/info`** includes `ibc_channel_count` — the number of unique live channel pairs across
all configured chains.

**Drip request:**

```sh
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1","address":"osmo1..."}'
```

**Response when `batch_window > 0` (default):** the request is queued and the response is immediate.

```json
{
  "drip_id": 42,
  "status": "queued",
  "amount": "1000000uosmo",
  "mechanism": "anonymous"
}
```

The drip record transitions `queued → submitted → confirmed` (or `failed`) as the batch is
broadcast and confirmed. Set `batch_window: "0"` to use synchronous mode and wait for
confirmation before the response is returned.

**Response in synchronous mode (`batch_window: "0"`):**

```json
{
  "drip_id": 1,
  "status": "confirmed",
  "amount": "1000000uosmo",
  "mechanism": "anonymous",
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

## E2E tests

The `e2e/` module runs integration tests against real Docker containers (ibc-go-simd chains and a
Hermes relayer). Docker must be running. Supply the binary to test against via one of:

| Variable           | Description                                                                                                                             |
|--------------------|-----------------------------------------------------------------------------------------------------------------------------------------|
| `POUR_BIN`         | Path to a pre-built `pour` binary (e.g. `../pour` after `make build`).                                                                  |
| `POUR_VERSION`     | Release tag to `go install` (e.g. `v0.5.0`). Mutually exclusive with `POUR_BIN`.                                                        |
| `POUR_E2E_VERBOSE` | Set to any non-empty value to stream Hermes relayer logs to stderr. Off by default — only needed when debugging IBC handshake failures. |

```sh
make build && cd e2e && POUR_BIN=../pour go test -v -timeout 20m ./...
```

## Roadmap

- [x] **v0.1.0** — single-chain drip, embedded UI, IP rate limiting
- [x] **v0.2.0** — chain registry integration, multi-chain runtime, admin API
- [x] **v0.3.0** — batch window, multiple distributor wallets, gRPC endpoint failover
- [x] **v0.4.0** — PoW challenge, API keys, signed-wallet authentication
- [x] **v0.5.0** — IBC plumbing
- [x] **v0.6.0** — IBC drips
- [x] **v0.7.0** — local devnet auto-configure (`pour serve --auto --home`)
- [ ] **v0.8.0** — REST/LCD transport (chains with gRPC-only or REST-only endpoints; gRPC→REST failover)
- [ ] **v1.0.0** — stable: API and config schema frozen under semver guarantees

## License

Apache-2.0 — see [LICENSE](LICENSE).