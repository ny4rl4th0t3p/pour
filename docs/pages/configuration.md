# chains.yml reference

The main configuration file. Copy `chains.yml.example` and edit it.
Set `POUR_CONFIG` to use a different path (default: `chains.yml`).

## Top-level structure

```yaml
abuse:      # global abuse-prevention settings
registry:   # chain registry fetch settings (optional)
admin:      # admin API access control (optional)
chains:     # list of chain configurations
```

---

## `abuse`

Controls the admission gate applied to every drip request.
All mechanisms default to `false` — enable only what you need.

```yaml
abuse:
  pow:
    enabled: true
    difficulty: medium        # easy | medium | hard | <positive integer>

  api_keys:
    enabled: true

  signature_challenge:
    enabled: false
    require_predicate: none   # none | has_balance
    # predicate_chain_id: ""  # chain to query; defaults to the chain being dripped
    # predicate_min_amount: "" # required when require_predicate: has_balance

  ip_rate_limit:
    requests_per_window: 10
    window: 1h                # Go duration: 1h, 30m, 5m, etc.
```

See [Abuse & auth](abuse.md) for how the mechanisms interact.

---

## `registry`

```yaml
registry:
  base_url: ""           # defaults to cosmos/chain-registry on GitHub
  refresh_interval: "6h" # how often to re-fetch chain metadata
```

Set `refresh_interval: "0"` to disable automatic refreshes (manual only via `POST /admin/registry/refresh`).

---

## `admin`

```yaml
admin:
  allowed_cidrs:
    - "127.0.0.1/32"    # default: localhost only
    - "10.0.0.0/8"      # add subnets as needed
```

---

## `chains[]`

Each entry in the `chains` list is either a **registry chain** or a **standalone chain**.

### Registry chain

Chain metadata (bech32 prefix, slip44, endpoints, fee tokens) is pulled from the public registry.
Only drip policy and optional overrides are needed.

```yaml
chains:
  - chain_id: osmosis-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"          # amount for anonymous / PoW / API-key requests
      signed: "5000000uosmo"             # amount for signed-wallet requests (optional)
      max_per_address_per_day: "50000000uosmo"

    # Concurrency (optional — defaults shown)
    distributors: 1                      # signing accounts, key indices 1..N; holder is 0
    batch_window: "5s"                   # coalesce into MsgMultiSend; "0" = synchronous
    max_recipients_per_batch: 25         # max outputs per MsgMultiSend
    # max_queue_depth: 500               # per-distributor request queue cap
    # refill_threshold: ""              # min distributor balance; default: drip.anonymous × 10
    # refill_interval: "1m"             # how often to check distributor balances

    # IBC drips (optional) — offer IBC vouchers sourced from another chain
    # ibc:
    #   timeout: 10m                  # MsgTransfer timeout; default 10m
    #   drips:
    #     - source_chain_id: cosmoshub-4
    #       anonymous: "1000000uatom"
    #       max_per_address_per_day: "10000000uatom"
```

### Standalone chain

For chains not in the public registry (local devnets, private testnets).
All fields must be provided explicitly.

```yaml
chains:
  - chain_id: mynet-1
    standalone: true
    enabled: true
    bech32_prefix: mynet
    slip44: 118
    endpoints:
      grpc:
        - localhost:9090              # one or more gRPC endpoints
      # rest:                         # REST/LCD endpoints (use instead of or alongside grpc)
      #   - http://localhost:1317
    fee_tokens:
      - denom: umynet
        low_gas_price: "0.001"
        average_gas_price: "0.025"
        high_gas_price: "0.04"       # optional
    drip:
      anonymous: "1000000umynet"
      max_per_address_per_day: "10000000umynet"
    batch_window: "0s"               # recommended for single-node devnets
```

### IBC destination chain

Pour holds tokens on the source chain and delivers them via `MsgTransfer`. The source chain
does not need a `drip` block — omitting it makes it source-only, so it broadcasts
`MsgTransfer` but does not serve native drips itself. The destination chain can be IBC-only
(no endpoints or native wallet) or carry both native and IBC drips simultaneously —
controlled per-request via the `denom` field.

```yaml
chains:
  - chain_id: osmosis-1             # source — holds tokens, fires MsgTransfer
    enabled: true                   # no drip block: source-only, not a public faucet

  - chain_id: mynet-1               # destination — receives IBC vouchers
    enabled: true
    ibc:
      timeout: 10m
      drips:
        - source_chain_id: osmosis-1
          anonymous: "1000000uosmo"
          max_per_address_per_day: "10000000uosmo"
```

See [IBC drips](ibc.md) for the full reference including dual native+IBC chains.

---

## Field reference

### `chain_id`

String. Cosmos chain ID. Must match the chain's genesis.

### `enabled`

Boolean. Set to `false` to stop serving drips for this chain without removing it from the config.

### `standalone`

Boolean. When `true`, the chain is not looked up in the public registry. All fields must be set manually.

### `bech32_prefix`

String. Address prefix (e.g. `osmo`, `cosmos`). Required for standalone chains.

### `slip44`

Integer. BIP44 coin type. `118` for most Cosmos chains. Required for standalone chains.

### `endpoints`

```yaml
endpoints:
  grpc:
    - host:port           # gRPC endpoint (no scheme)
    - host2:port
  rest:
    - http://host:port    # REST/LCD endpoint (with scheme)
```

Multiple endpoints form a pool. pour rotates through them on failure.
See [Transports](transports.md) for failover behaviour.

### `fee_tokens[]`

```yaml
fee_tokens:
  - denom: uosmo
    low_gas_price: "0.01"
    average_gas_price: "0.025"
    high_gas_price: "0.04"
```

Pour uses `average_gas_price` for fee estimation. Required for standalone chains.

### `drip`

| Field                     | Description                                                                                                                        |
|---------------------------|------------------------------------------------------------------------------------------------------------------------------------|
| `anonymous`               | Drip amount for anonymous, PoW, and default API-key requests. Coin string, e.g. `"1000000uosmo"`. Required for native drip chains. |
| `signed`                  | Drip amount for signed-wallet requests. Optional — falls back to `anonymous`.                                                      |
| `max_per_address_per_day` | Per-address daily cap. Required when `anonymous` is set.                                                                           |

Omit the entire `drip` block to make a chain source-only: it can broadcast `MsgTransfer`
for other chains' IBC drips but will not serve native drip requests.

API keys can individually override the drip amount via `per_chain_drips` at creation time; keys without an override
inherit `anonymous`. See [Abuse & auth — API keys](abuse.md#api-keys).

### `distributors`

Integer. Number of distributor signing accounts (key indices 1 to N). The holder account is always key index 0. Default:
`1`.

Distributors reduce sequence-number contention under high load. Each distributor maintains its own sequence counter. The
holder account periodically refills distributors when their balances fall below `refill_threshold`.

### `batch_window`

Go duration string. How long pour waits to accumulate requests before broadcasting a `MsgMultiSend`.

- `"5s"` — default; good for production
- `"0"` or `"0s"` — synchronous mode: each request broadcasts immediately and the response waits for confirmation

### `max_recipients_per_batch`

Integer. Maximum outputs in one `MsgMultiSend`. Default: `25`.

### `max_queue_depth`

Integer. Maximum requests queued per distributor. Requests beyond this cap receive `503`. Default: `500`.

### `refill_threshold`

Coin string. Minimum distributor balance before the holder triggers a refill.
Default: `drip.anonymous × 10`.

### `refill_interval`

Go duration string. How often the refill loop checks distributor balances. Default: `1m`.