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

| Policy | Fields | Behaviour |
|--------|--------|-----------|
| **Freeze** | `chain_id`, `chain_name`, `network_type`, `bech32_prefix`, `slip44`, `key_algo`, `fee_tokens.denom` | Blocked and queued as a pending change. Not applied until the operator explicitly accepts it. |
| **Warn** | `fee_tokens.low_gas_price`, `fee_tokens.average_gas_price`, `fee_tokens.high_gas_price` | Applied immediately; a warning is logged so operators can review. |
| **Hot-reload** | `endpoints.grpc`, `endpoints.rpc`, `endpoints.rest`, `fee_tokens.display`, `fee_tokens.exponent`, `pretty_name`, `block_time` | Applied silently and immediately. |

Freeze fields are those that would break address derivation, transaction signing, or fee estimation if applied without review. Any unrecognised field also defaults to Freeze.

**Config overrides bypass the policy entirely.** If a field is explicitly set in `chains.yml` (e.g. `bech32_prefix` or `endpoints`), pour uses the config value and ignores registry updates for that field — no pending change is ever created for it.

```sh
# See what's waiting
curl -s http://localhost:8080/admin/registry/pending \
  -H "Authorization: Bearer $TOKEN" | jq .

# Accept all pending changes for osmosis-1
curl -X POST http://localhost:8080/admin/registry/accept \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1"}'

# Accept only the bech32_prefix field
curl -X POST http://localhost:8080/admin/registry/accept \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"osmosis-1","field":"Bech32Prefix"}'
```

### Config reload

| Method | Path            | Description                                                        |
|--------|-----------------|--------------------------------------------------------------------|
| `POST` | `/admin/reload` | Re-read `chains.yml` and apply drip policy changes without restart |

Changes to drip amounts, batch windows, and abuse settings take effect immediately. Adding a brand
new chain that was absent at startup requires a full process restart.

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

### Chain status

| Method | Path                           | Description                                   |
|--------|--------------------------------|-----------------------------------------------|
| `GET`  | `/admin/chains/{chain}/status` | Suspended flag, fail streaks, multisend state |
| `POST` | `/admin/chains/{chain}/resume` | Clear suspension and reset fail streaks       |

A chain is automatically suspended after 5 consecutive send failures. While suspended, all drip
requests for that chain return `503`. Resume manually after diagnosing the cause:

```sh
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

See [Abuse & auth — API keys](abuse.md#api-keys) for key management examples.

```sh
# Rotate the admin token (old token rejected immediately)
curl -X POST http://localhost:8080/admin/api-keys/rotate-admin \
  -H "Authorization: Bearer $TOKEN"
# → {"token":"pour_admin_..."}  store before closing the connection
```