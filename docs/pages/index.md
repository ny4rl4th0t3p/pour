# pour

A pure-Go, multi-chain Cosmos faucet. Single static binary — no Node, no CGO, no cosmos-sdk import, no shelling out to
chain CLIs.

Builds and broadcasts transactions via raw protobuf over gRPC or REST, speaking directly to chain nodes without any
SDK wrapper. Chains are sourced from the [cosmos/chain-registry](https://github.com/cosmos/chain-registry) and cached
locally; standalone mode is available for chains not in the public registry.

## Why pour?

Most Cosmos faucets are wrappers around the chain CLI binary — they shell out to `gaiad tx bank send`, depend on
Node.js for the web UI, or require the cosmos-sdk as a Go library. pour takes a different approach:

- **No runtime dependencies.** One `go install` produces a fully static binary. No Node, no Python, no chain binary
  on PATH, no shared libraries. Drop it on any Linux or Mac host and it runs.
- **Raw protobuf, no SDK.** Transactions are assembled and signed in pure Go using raw protobuf types. There is no
  cosmos-sdk import, which means no dependency conflicts, no SDK version lock-in, and a fraction of the binary size.
- **Devnet-first automation.** `pour serve --auto --home ~/.simapp` reads the local genesis file, derives chain ID,
  bech32 prefix, and native denom, generates a faucet key, optionally self-funds from a genesis account, and starts
  serving — with zero manual config. This level of automation is rare in Cosmos tooling: most faucets require a full
  `chains.yml` even for a local devnet.
- **Production-grade under load.** Batch `MsgMultiSend` windows, multiple distributor wallets, per-distributor
  sequence tracking, and gRPC→REST failover are built in — not bolted on.

## Key features

- **Single binary, zero deps** — pure Go, no CGO, no Node, no cosmos-sdk
- **Multi-chain** — one process serves any number of chains simultaneously
- **Auto mode for devnets** — self-configures from genesis; works with `ignite chain serve` and `simd start`
- **Hot reload** — detects chain resets (block height regression) and reconnects without a faucet restart
- **IBC drips** — hold tokens on a source chain and IBC-transfer to destination chains on demand
- **Batch sending** — coalesces requests into `MsgMultiSend`; multiple distributor wallets reduce contention under load
- **gRPC + REST transports** — gRPC by default; automatic failover to REST/LCD when gRPC is unavailable
- **Abuse prevention** — layered gate: API keys, signed Cosmos wallet challenges, Altcha PoW, IP rate limiting
- **Admin API** — manage keys, inspect gas cache, resume suspended chains, rotate tokens — no restart required

## Quick links

- [Installation & quickstart](getting-started.md)
- [Auto mode for devnets](auto-mode.md)
- [chains.yml reference](configuration.md)
- [Abuse & auth](abuse.md)
- [Admin API](admin-api.md)
- [OpenAPI spec](https://github.com/ny4rl4th0t3p/pour/blob/main/openapi.yaml)

## Status

> **Active development.** The API, config schema, and CLI flags are not stable until v1.0.0.
> Pre-1.0 minor releases may add config keys but aim not to remove or rename existing ones.

| Version | Milestone                                                                                              |
|---------|--------------------------------------------------------------------------------------------------------|
| v0.1.0  | Single-chain drip, embedded UI, IP rate limiting                                                       |
| v0.2.0  | Chain registry integration, multi-chain runtime, admin API                                             |
| v0.3.0  | Batch window, multiple distributor wallets, gRPC endpoint failover                                     |
| v0.4.0  | PoW challenge, API keys, signed-wallet authentication                                                  |
| v0.5.0  | IBC plumbing                                                                                           |
| v0.6.0  | IBC drips                                                                                              |
| v0.7.0  | Local devnet auto-configure (`pour serve --auto --home`)                                               |
| v0.8.0  | REST/LCD transport, gRPC→REST failover                                                                 |
| v0.8.1  | Documentation site launch, refill bug fixes                                                            |
| v0.8.2  | OpenAPI spec accuracy, admin CLI completeness (`chains status`, `api-keys` create flags + list fields) |
| v0.8.3  | *(planned)* smoke test coverage for admin API key endpoints                                            |