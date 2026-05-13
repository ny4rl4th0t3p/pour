# pour

> **This project is under active development.** The API, config schema, and CLI flags are not stable until v1.0.0.
> Pre-1.0 minor releases may add new config keys but aim not to remove or rename existing ones.

A pure-Go, multi-chain Cosmos faucet. Single static binary, no Node, no CGO, no shelling out to chain CLIs. Builds and
broadcasts transactions via raw protobuf over gRPC or REST — no cosmos-sdk import required.

*One of three operator tools I've built for Cosmos SDK chains. The others are
[gentool](https://github.com/ny4rl4th0t3p/cosmos-genesis-tool) (deterministic
genesis file generation) and [chaincoord](https://github.com/ny4rl4th0t3p/chaincoord)
(multi-party launch coordination). Together they cover the painful workflows of
launching and running a Cosmos chain. All three are Apache 2.0 and self-hostable.*

**Highlights:**

- Chains sourced from [cosmos/chain-registry](https://github.com/cosmos/chain-registry) and cached locally — add a
  chain by ID with no manual metadata
- Standalone mode for chains not in the public registry (local devnets, private testnets)
- Multiple distributor wallets reduce sequence-number contention under high load; holder auto-refills them at startup
- Priority-ordered abuse gate: API key → signed wallet (ADR-036) → proof-of-work (Altcha) → anonymous
- Per-address daily cap keyed on raw address bytes, so a chain prefix migration cannot reset a user's allowance
- Embedded web UI, Prometheus metrics, hot-reload config, admin API

📖 **[Full documentation](https://ny4rl4th0t3p.github.io/pour)**

---

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

# Set the mnemonic and run
export POUR_MNEMONIC="word word word ..."
pour serve
```

Fund the address derived from `POUR_MNEMONIC` on each chain. Distributors are topped up from
the holder automatically at startup.

**Local devnet (`ignite chain serve`, `simd start`, etc.)**

```sh
# Auto-configure from genesis; self-fund from a genesis account
pour serve --auto --home ~/.simapp --fund-mnemonic "word word word ..."
```

Pour infers the chain ID, bech32 prefix, and native denom from the genesis file. See
[Auto mode](https://ny4rl4th0t3p.github.io/pour/auto-mode) for the full flag reference.

## Key environment variables

| Variable           | Default            | Description                                         |
|--------------------|--------------------|-----------------------------------------------------|
| `POUR_MNEMONIC`    | —                  | **Required.** BIP39 mnemonic for the faucet wallet. |
| `POUR_CONFIG`      | `chains.yml`       | Path to the chains config file.                     |
| `POUR_LISTEN`      | `:8080`            | Address to listen on.                               |
| `POUR_LOG_LEVEL`   | `info`             | `debug` \| `info` \| `warn` \| `error`              |
| `POUR_METRICS`     | `false`            | Enable Prometheus metrics at `/metrics`.            |
| `POUR_ADMIN_TOKEN` | *(auto-generated)* | Admin API bearer token.                             |

See [Configuration](https://ny4rl4th0t3p.github.io/pour/configuration) and
[Getting started](https://ny4rl4th0t3p.github.io/pour/getting-started) for the full reference.

## E2E tests

Runs against real Docker containers (ibc-go-simd chains + Hermes relayer):

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
- [x] **v0.8.0** — REST/LCD transport (gRPC→REST failover)
- [x] **v0.8.1** — documentation site launch, refill bug fixes
- [x] **v0.8.2** — OpenAPI spec accuracy, admin CLI completeness (`chains status`, `api-keys` create flags + list
  fields)
- [ ] **v0.8.3** — smoke test coverage for admin API key endpoints
- [ ] **v1.0.0** — stable: API and config schema frozen under semver guarantees

## License

Apache-2.0 — see [LICENSE](LICENSE).