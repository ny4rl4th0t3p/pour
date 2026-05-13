# pour — design

A pure-Go, multi-chain Cosmos faucet. This document explains the design decisions behind it, the alternatives that were
considered, and the consequences of each choice. It is not a user guide; for that, see the README and
the [documentation site](https://ny4rl4th0t3p.github.io/pour).

## Context

Every Cosmos chain needs a faucet during its testnet phase, and most chains need one for their development testnets
indefinitely. Despite this universal need, the state of the art for Cosmos faucets in 2026 is uneven. The
implementations in active use share a common set of problems:

- **SDK coupling or CLI dependency.** Most implementations either vendor a specific cosmos-sdk version (so a new SDK
  release requires a code change to support) or shell out to chain CLIs to avoid the coupling. Either approach requires
  that the exact chain binary, in the right version, be present in the deployment environment.
- **Single-chain scope.** Most handle a single chain per instance, so operators running multiple testnets typically
  stand up one deployment per chain.
- **Sequence-number contention.** Many implementations have no multi-distributor strategy; the single faucet wallet
  fails to broadcast under a sustained load because of sequence-mismatch rejections. Some implementations solve this
  with
  a multi-distributor pattern (token holder funding N distributor accounts that issue drips independently); pour adopts
  this pattern.
- **Coarse abuse protection.** The widespread combination is captcha plus per-IP rate limiting, sometimes plus
  per-address cooldown. These rarely combine into a tiered model where different user populations (CI bots, developers
  with wallets, anonymous web users) get appropriate friction.
- **Address-string caps.** Most implementations key the per-address cap on the bech32 string, which breaks when a
  chain migrates its prefix: the same key gets a fresh allowance under the new prefix.

pour was built to address these in a single Go binary with no language runtime, no chain-binary dependency, and
operator-friendly defaults.

## What this is

`pour` is a faucet daemon that drips testnet tokens to requesters, scoped to one or more Cosmos SDK chains, with
operator-friendly defaults and production-grade anti-abuse.

**In scope:**

- Single-binary deployment, no language runtime, no CGO, no shelling out
- Multi-chain support driven by the public `cosmos/chain-registry`
- Native chain support including chains not in the public registry
- IBC-aware drips (token transfers across chains via IBC)
- Tiered abuse protection with sensible defaults
- Production operator tooling (hot reload, admin API, Prometheus metrics, OpenAPI spec, structured logs)
- Local-devnet autoconfiguration for chain teams running `simd`, `ignite`, or any local chain tooling

**Out of scope:**

- Mainnet token distribution
- Custodial wallet management beyond what's strictly required for faucet operation
- A public-hosted service (the project is the software; hosting is the operator's responsibility)

## Key design decisions

### 1. Pure Go, no `cosmos-sdk` import

**Alternatives considered:**

- Vendor a specific Cosmos SDK version and import its transaction-building primitives
- Shell out to the chain's CLI binary for each transaction broadcast
- Depend on third party software and run a sidecar runtime

**Choice:** Build and broadcast transactions via raw protobuf, talking to gRPC and REST endpoints directly.

**Rationale:** The Cosmos SDK is on a multi-year breaking-change cycle. Any faucet that imports the SDK is tied to one
version; supporting a set of SDK versions requires either multiple binaries or version-shimming gymnastics that no
single repository can sustain. Shelling out to chain CLIs requires the operator to have the exact CLI binary present, in
the correct version, on PATH — a real burden when running many testnets across many SDK versions. Raw protobuf is stable
across SDK versions; the message types in `cosmos.bank.v1beta1`, `cosmos.tx.v1beta1`, and the IBC namespaces have not
broken in years.

**Consequences:**

- Transaction construction is implemented manually, including fee estimation, sequence management, signing, and
  broadcast. This is more work upfront.
- The tool is genuinely portable across SDK versions; a new chain on a newer SDK version usually requires zero changes.
- The binary is small and has no runtime dependencies beyond the OS.
- The tx layer supports both gRPC and REST transports; when a chain exposes no gRPC endpoint the daemon uses REST.
- An adaptive gas-price cache (per chain, SQLite-backed, with exponential decay) absorbs transient network-side gas
  spikes without repeated on-chain simulation queries.
- Any chain that changes its bech32 prefix, denom, or transaction format in a non-standard way (some chains do) requires
  explicit handling, but this is the rare case rather than the common one.

### 2. Chain-registry-driven configuration

**Alternatives considered:**

- Require the operator to write a full chain config (chain ID, RPC URL, bech32 prefix, base denom, gas price, fee
  config) per chain
- Bundle a static list of supported chains compiled into the binary
- Use a custom config registry maintained alongside the project

**Choice:** Read chain metadata from `cosmos/chain-registry`, resolved at startup and refreshed periodically in memory,
with operator overrides as needed.

**Rationale:** The community-maintained `cosmos/chain-registry` is the canonical source of truth for Cosmos chain
metadata. Reimplementing or duplicating it produces drift. The operator should be able to add a chain by writing the
chain ID and have the daemon figure out the rest from the registry.

**Consequences:**

- Adding a new public chain typically requires only the chain ID; everything else is resolved automatically.
- The faucet remains functional for chains not in the public registry through a standalone-mode chain definition.
- The resolved registry data is kept in memory. Within a single process run, a refresh failure falls back to the last
  known in-memory state. On restart, the registry must be reachable for the initial fetch only when registry-backed
  chains are configured; standalone-only and auto-mode deployments start without any registry access.
- The operator can override registry-supplied values when needed (e.g., a specific RPC endpoint that's more reliable
  than the registry's default).

### 3. Multiple distributor wallets with holder auto-refill

**Alternatives considered:**

- Single faucet wallet, sequential broadcasting
- Single wallet with optimistic sequence management
- Manual operator-managed pool of wallets

**Choice:** A holder wallet that, at startup, derives or funds N distributor wallets. Drips are issued from distributors
in rotation. The holder refillsdistributors automatically when they fall below a threshold.

**Rationale:** Sequence-number contention is the most common failure mode of Cosmos faucets under load. A single wallet
broadcasting concurrent transactions hits the mempool's sequence-mismatch rejection regularly. Sequential broadcasting
solves correctness but caps throughput at one transaction per block. Multiple distributors give the daemon a pool of
independent sequence streams. Automatic refill keeps the operator out of the loop for normal operations; without it, a
long-running faucet under a sustained load eventually drains its distributors and starts failing requests until an
operator manually tops them up.

**Consequences:**

- Throughput under sustained load scales with N: each distributor has its own independent sequence stream, so N
  transactions can be in-flight per block rather than one.
- Batched sends use `MsgMultiSend` (multiple recipients per transaction) to reduce the chain load further. If
  `MsgMultiSend` is persistently rejected, the daemon degrades gracefully to individual `MsgSend` per recipient and
  disables multi-send for that chain automatically.
- Auto-refill keeps the operator out of the loop for normal operations.
- The holder's balance becomes the operational risk surface; distributors are short-lived buckets.
- After five consecutive broadcast failures, a chain auto-suspends to prevent cascading errors from reaching the
  user-facing HTTP layer. The operator resumes it via `pour admin chains resume <chain_id>` (or directly via
  `POST /admin/chains/<chain_id>/resume`).

### 4. Priority-ordered abuse gate

**Alternatives considered:**

- Per-IP rate limiting only
- Captcha for all requests
- Signed-wallet-only access (close to no friction for some, locked out for others)
- API-key-only access

**Choice:** A priority chain — API key → ADR-036 signed wallet → proof-of-work challenge → anonymous — where each
request is served at the lowest-friction tier the requester can satisfy.

**Rationale:** Different users have different abuse profiles and different tolerable friction. A CI bot cannot solve a
captcha but can present a stable API key. A developer can sign a message with their existing wallet at near-zero cost. A
casual web user expects to get tokens with at most a captcha-like challenge. A single mechanism fails one of these
populations. The priority chain serves each at its appropriate tier.

**Consequences:**

- The configuration surface is larger (each tier has its own rate limits and quotas)
- The operator can disable tiers they don't want to expose; an anonymity-blocked deployment turns off the anonymous tier
  entirely
- The taxonomy is opinionated; operators with different threat models may want to reorder or add tiers, which the design
  permits but does not pre-build

### 5. Hot-reload configuration

**Alternatives considered:**

- Restart-on-config-change
- API-only configuration changes
- File watcher with no reload semantics (config read on each request)

**Choice:** A `POST /admin/reload` call (accessible as `pour admin chains reload`) that re-reads `chains.yml` and
applies any safe changes without restarting in-flight requests.

**Rationale:** A faucet operator should be able to add a chain, adjust a rate limit, or rotate a distributor wallet
without taking the service offline. Restart-on-change is a 30-second outage every time the operator touches anything.
API-only configuration creates a different operational footprint (now there's an admin API to manage as the source of
truth) that's a worse default for a tool that's mostly file-config-driven.

**Consequences:**

- The holder mnemonic is supplied via environment variable or auto-mode file, not `chains.yml`, so rotating the
  mnemonic always requires a restart — `POST /admin/reload` does not re-initialize existing chain connections.
- Newly added registry chains absent from the initial fetch are not hot-reloaded; a restart is required to pick them up.
- The reload semantics are explicit per config key; the docs state which keys are hot-reloadable and which require
  restart.
- In-flight requests are not interrupted.

### 6. Admin API with bearer token authentication

**Alternatives considered:**

- No admin API (file-only configuration)
- Admin API on the same port and route prefix as the public faucet
- Admin API on a separate port

**Choice:** Admin API on the same listener but under a separate route prefix, protected by two layers: an IP allowlist
(defaulting to loopback only) and bearer token authentication on every admin route. The token is resolved in priority
order: `.pour-admin-token` file → `POUR_ADMIN_TOKEN` env var → auto-generated and written to `.pour-admin-token`
(mode `0600`) at first startup. The file takes priority over the env var so that rotations survive process restarts.

**Rationale:** Operators need a programmatic way to manage state during incidents (drain a distributor, freeze a tier,
force a refill). A separate port is operationally cleaner but adds firewall complexity for deployments behind reverse
proxies. The same port with route-based separation is the common pattern (Prometheus, Grafana, etc.) and works behind
any reverse proxy.

**Consequences:**

- The default IP allowlist restricts admin access to loopback. Operators exposing the daemon behind a reverse proxy
  must configure `admin.allowed_cidrs` to include the proxy's source IP.
- A fresh installation is protected immediately: the admin endpoint is loopback-only, and the auto-generated token is
  written with restrictive permissions before the HTTP listener opens.
- Token rotation is supported and documented.

### 7. Embedded web UI

**Alternatives considered:**

- API-only, leave UI to operators
- Separate UI repository
- Embedded UI with embedded asset bundling

**Choice:** A minimal UI embedded in the binary via `go:embed`, served at the root.

**Rationale:** A faucet without a UI is unusable for the casual web-arriving user, who is a real population. Operators
don't want to deploy and maintain a separate frontend service for a tool that is itself fundamentally simple. Embedding
the UI in the binary means there's one thing to deploy and one thing to version.

**Consequences:**

- The binary is slightly larger
- UI changes ship with daemon releases, not independently
- The UI is intentionally minimal; operators who want a branded experience can disable it and serve their own from a
  reverse proxy or upgrade the existing one

### 8. Standalone mode for non-public-registry chains

**Alternatives considered:**

- Public-registry-only support
- Custom-registry support (private fork of `cosmos/chain-registry`)
- Standalone YAML chain definitions

**Choice:** Standalone-mode chain definitions in `chains.yml` that match the public registry's schema but live entirely
in the operator's config.

**Rationale:** Local devnets, private testnets, customer-specific chains, and chains too new to be in the public
registry are all common. Forcing the operator to push to `cosmos/chain-registry` for a 10-validator private testnet is
wrong. The standalone definition is the same shape as a registry entry, so the mental model is identical.

**Consequences:**

- A chain team running a private testnet writes one chain config in `chains.yml` and has a working faucet
- The same definition can be promoted to a registry contribution later if the chain goes public
- Migration is automatic: if a chain ID later appears in the public registry, the operator can switch sources without
  changing the daemon's behavior

### 9. Local-devnet auto-configuration (`--auto --home ~/.simapp`)

**Alternatives considered:**

- Always require explicit chain config
- Detect chain metadata from a node's RPC endpoint
- Read chain metadata from a node's genesis file on disk

**Choice:** When `--auto --home <path>` is passed, derive chain ID, bech32 prefix, native denom, and signing config
directly from the node's local genesis file and binary defaults.

**Rationale:** A chain developer running `ignite chain serve` or `simd start` locally should be able to point the faucet
at the node and have it work. Asking the developer to read the genesis file, extract the chain ID and prefix, write a
config, and start the faucet is friction that drives them to write their own quick-and-dirty faucet instead.

**Consequences:**

- The full developer experience is `pour serve --auto --home <home>`; one command, faucet works
- The detection logic reads standard cosmos-sdk genesis paths (chain_id, staking params, bank balances). Chains that
  deviate from these paths may require explicit config overrides via --drip or chains.yml.
- The flag combination is intentionally not the default; a production operator running against a remote chain should not
  have an auto-discovery firing

### 10. Registry field-change policy

**Alternatives considered:**

- Accept all live-registry updates immediately
- Treat all field changes as dangerous and require operator acceptance for everything
- Log changes but take no action

**Choice:** A three-tier policy classifies each registry field by risk level. Low-risk fields (endpoints, block time,
display metadata) are hot-reloaded silently on the next refresh. Medium-risk fields (gas prices) are applied
immediately with a log warning. High-risk fields (chain ID, chain name, network type, bech32 prefix, key algorithm,
fee denom, SLIP-44) are queued as pending changes and not applied until the operator explicitly accepts them via
`pour admin chains accept`.

**Rationale:** A `cosmos/chain-registry` entry can be updated by anyone who merges a PR. Most changes are routine
(new RPC endpoints, updated gas prices). A few — particularly bech32 prefix and fee denom — would silently break the
faucet or cause it to broadcast transactions with a wrong denomination if applied automatically. The three-tier policy
absorbs routine changes immediately while requiring explicit human sign-off on breaking ones.

**Consequences:**

- Operators never need to act on routine registry updates; the daemon absorbs them automatically.
- Breaking-field changes surface as entries in `pour admin chains pending`; the faucet keeps running on the last
  accepted values until the operator is ready to accept the change.
- Only fields in the hardcoded classifiableFields list are policy-checked. Fields added to the registry schema in the
  future would need to be explicitly added.
- The freeze policy only applies to live updates during a running session. On daemon restart, the initial registry
  fetch populates the store directly with no policy check — changes that accumulated during the downtime are absorbed
  without operator review. Operators who need strict freeze coverage across restarts should pin the relevant fields in
  `chains.yml` using `pour admin chains pin`.

## Known limitations

- **Freeze policy does not survive daemon restarts.** On the initial registry fetch after a restart, all fields —
  including Freeze-policy fields — are applied directly with no policy check, because there is no persisted baseline
  to diff against. The `pending_changes` SQLite table exists as scaffolding for this fix but is not yet wired to the
  registry store. Until it is, operators who require strict Freeze coverage should pin sensitive fields in `chains.yml`
  using `pour admin chains pin`; config overrides are immune to both live-update and restart-time policy bypass.
- **Cosmos SDK chains only.** Non-Cosmos chains (Solana, Ethereum, etc.) are out of scope.
- **secp256k1 signing only.** Some chains use other curves (ethsecp256k1 for some EVM-compatible chains, ed25519 for
  some specialized chains); these are not currently supported.
- **No web-of-trust or KYC integration.** The daemon does not, and will not, run KYC checks on requesters. Operators
  with regulatory requirements should run the faucet behind an authentication layer that handles compliance externally.
- **No on-chain accounting beyond the faucet's own state.** The daemon does not track distributions in a transparent
  on-chain ledger. Every drip is recorded locally in the SQLite `drips` table (address, coins, tx hash, status,
  mechanism, timestamps); logs and Prometheus metrics provide secondary observability.
- **Per-address cap uses local state.** The daily cap is enforced against a local SQLite database, which persists across
  daemon restarts. Multiple daemon instances do not share this state.

## What's next

The roadmap to v1.0 emphasizes stability and operational maturity rather than new features:

- Frozen config schema and HTTP API surface under semver guarantees
- Persistent freeze-policy baseline: wire `chainregistry.Store` to the existing `pending_changes` SQLite table so
  the initial fetch on restart diffs against the last-accepted state rather than applying all fields unconditionally
- Expanded E2E coverage for key transport paths (gRPC, REST, failover), auth mechanisms, and abuse gate tiers
- Documentation accuracy audit: known divergences exist between the documented behavior and the actual implementation
  (source paths in auto-mode, field-policy coverage, signing curve handling, audit trail). A systematic pass is needed
  before the API surface is frozen under semver guarantees

After v1.0, the roadmap is deliberately demand-driven. If you are an operator, chain team, or institution with a
specific need, open an issue — the project is actively maintained, and real use cases take priority over speculative
features.
