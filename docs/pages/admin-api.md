# Admin API

The admin API provides operator endpoints for runtime management. It is mounted at `/admin` and
restricted to localhost by default (configurable via `admin.allowed_cidrs`).

## Authentication

All admin endpoints require a bearer token:

```sh
curl http://localhost:8080/admin/... \
  -H "Authorization: Bearer $(cat .pour-admin-token)"
```

See [Getting started — Admin token](getting-started.md#admin-token) for how the token is resolved
and rotated.

## Full API reference

The complete request/response schema is in [`openapi.yaml`](../../openapi.yaml) at the repo root.
You can render it locally with any OpenAPI viewer (Swagger UI, Redoc, etc.):

```sh
npx @redocly/cli preview-docs openapi.yaml
```

## Endpoint summary

### Registry

| Method | Path                       | Description                                                    |
|--------|----------------------------|----------------------------------------------------------------|
| `GET`  | `/admin/registry/snapshot` | Full resolved view of all chains in the registry store         |
| `GET`  | `/admin/registry/pending`  | Field-level changes awaiting operator acceptance               |
| `POST` | `/admin/registry/accept`   | Accept a pending change for one field or all fields of a chain |
| `POST` | `/admin/registry/refresh`  | Trigger an immediate registry re-fetch                         |

Pour applies registry updates according to a per-field policy:

| Policy         | Fields                                                                                                                        | Behaviour                                                                                     |
|----------------|-------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| **Freeze**     | `chain_id`, `chain_name`, `network_type`, `bech32_prefix`, `slip44`, `key_algo`, `fee_tokens.denom`                           | Blocked and queued as a pending change. Not applied until the operator explicitly accepts it. |
| **Warn**       | `fee_tokens.low_gas_price`, `fee_tokens.average_gas_price`, `fee_tokens.high_gas_price`                                       | Applied immediately; a warning is logged so operators can review.                             |
| **Hot-reload** | `endpoints.grpc`, `endpoints.rpc`, `endpoints.rest`, `fee_tokens.display`, `fee_tokens.exponent`, `pretty_name`, `block_time` | Applied silently and immediately.                                                             |

Freeze fields are those that would break address derivation, transaction signing, or fee estimation if applied without
review. Any unrecognised field also defaults to Freeze.

**Config overrides bypass the policy entirely.** If a field is explicitly set in `chains.yml` (e.g. `bech32_prefix` or
`endpoints`), pour uses the config value and ignores registry updates for that field — no pending change is ever created
for it.

```sh
# Inspect the full resolved registry state
pour admin chains list
curl -s http://localhost:8080/admin/registry/snapshot \
  -H "Authorization: Bearer $TOKEN" | jq .

# See what's waiting for acceptance
pour admin chains pending
curl -s http://localhost:8080/admin/registry/pending \
  -H "Authorization: Bearer $TOKEN" | jq .

# Accept all pending changes for osmosis-1
pour admin chains accept osmosis-1
curl -X POST http://localhost:8080/admin/registry/accept \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1"}'

# Accept only a specific field
pour admin chains accept osmosis-1 Bech32Prefix
curl -X POST http://localhost:8080/admin/registry/accept \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1","field":"Bech32Prefix"}'

# Accept all pending changes across all chains at once
pour admin chains accept --all

# Trigger an immediate registry re-fetch
pour admin chains refresh
curl -X POST http://localhost:8080/admin/registry/refresh \
  -H "Authorization: Bearer $TOKEN"
```

#### Diff and pin

Two read-only commands help manage config overrides without making live changes:

```sh
# Show which chains.yml overrides differ from the live registry snapshot
pour admin chains diff

# Emit a chains.yml snippet that pins a field to its current registry value
pour admin chains pin osmosis-1 Bech32Prefix
pour admin chains pin osmosis-1 Endpoints.GRPC
```

`diff` compares every field in `chains.yml` against the live daemon snapshot and prints a `field: old → new`
summary. Useful before accepting a pending change or after editing `chains.yml`.

`pin` generates a ready-to-paste YAML snippet for one field, using the value currently in the daemon.
Supported fields: `ChainName`, `NetworkType`, `KeyAlgo`, `Bech32Prefix`, `Slip44`, `BlockTime`,
`Endpoints.GRPC`, `Endpoints.RPC`, `Endpoints.REST`, `FeeTokens.LowGasPrice`, `FeeTokens.AverageGasPrice`,
`FeeTokens.HighGasPrice`.

### Config reload

| Method | Path            | Description                                                        |
|--------|-----------------|--------------------------------------------------------------------|
| `POST` | `/admin/reload` | Re-read `chains.yml` and apply drip policy changes without restart |

Changes to drip amounts, batch windows, and abuse settings take effect immediately. Adding a brand
new chain that was absent at startup requires a full process restart.

```sh
pour admin chains reload
curl -X POST http://localhost:8080/admin/reload \
  -H "Authorization: Bearer $TOKEN"
```

### Distributors

| Method | Path                                 | Description                                             |
|--------|--------------------------------------|---------------------------------------------------------|
| `GET`  | `/admin/distributors/{chain}`        | Live balances, queue depths, and status per distributor |
| `POST` | `/admin/distributors/{chain}/refill` | Trigger immediate refill from the holder account        |

```sh
# Check distributor state
curl -s http://localhost:8080/admin/distributors/osmosis-1 \
  -H "Authorization: Bearer $TOKEN" | jq .

# Refill all distributors below threshold
curl -X POST http://localhost:8080/admin/distributors/osmosis-1/refill \
  -H "Authorization: Bearer $TOKEN"

# Refill a specific distributor (key index 2)
curl -X POST http://localhost:8080/admin/distributors/osmosis-1/refill \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"index":2}'
```

### Gas cache

| Method | Path                                    | Description                                    |
|--------|-----------------------------------------|------------------------------------------------|
| `GET`  | `/admin/chains/{chain}/gas-cache`       | Current learned gas parameters                 |
| `POST` | `/admin/chains/{chain}/gas-cache/reset` | Clear the cache, forcing cold-start estimation |

Pour learns gas parameters from successful broadcasts and caches them per chain. Resetting the
cache is useful after a chain upgrade changes gas costs.

```sh
# Inspect the current cache entry
curl -s http://localhost:8080/admin/chains/osmosis-1/gas-cache \
  -H "Authorization: Bearer $TOKEN" | jq .

# Reset the cache (next broadcast will cold-start estimation)
curl -X POST http://localhost:8080/admin/chains/osmosis-1/gas-cache/reset \
  -H "Authorization: Bearer $TOKEN"
```

### Chain status

| Method | Path                           | Description                                   |
|--------|--------------------------------|-----------------------------------------------|
| `GET`  | `/admin/chains/{chain}/status` | Suspended flag, fail streaks, multisend state |
| `POST` | `/admin/chains/{chain}/resume` | Clear suspension and reset fail streaks       |

A chain is automatically suspended after 5 consecutive send failures. While suspended, all drip
requests for that chain return `503`. Resume manually after diagnosing the cause:

```sh
# Check current status (suspended flag, fail streaks, multisend state)
pour admin chains status osmosis-1
curl -s http://localhost:8080/admin/chains/osmosis-1/status \
  -H "Authorization: Bearer $TOKEN" | jq .

# Resume a suspended chain
pour admin chains resume osmosis-1
curl -X POST http://localhost:8080/admin/chains/osmosis-1/resume \
  -H "Authorization: Bearer $TOKEN"
```

### API keys

| Method   | Path                           | Description                                |
|----------|--------------------------------|--------------------------------------------|
| `POST`   | `/admin/api-keys`              | Issue a new API key (secret returned once) |
| `GET`    | `/admin/api-keys`              | List active keys (no secrets)              |
| `DELETE` | `/admin/api-keys/{id}`         | Revoke a key immediately                   |
| `POST`   | `/admin/api-keys/rotate-admin` | Rotate the admin bearer token              |

See [Abuse & auth — API keys](abuse.md#api-keys) for key management examples. CLI wrappers are
available for all four operations: `pour admin api-keys create/list/revoke/rotate`.

```sh
pour admin api-keys create --chain osmosis-1 --label ci-bot
pour admin api-keys list
pour admin api-keys revoke <id>
pour admin api-keys rotate
```

```sh
# Rotate the admin token — old token rejected immediately, new token returned once
curl -X POST http://localhost:8080/admin/api-keys/rotate-admin \
  -H "Authorization: Bearer $TOKEN"
# → {"token":"pour_admin_..."}  store before closing the connection
```