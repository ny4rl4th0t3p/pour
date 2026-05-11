# Abuse & auth

Pour evaluates each request through a priority-ordered admission gate. The **first** mechanism that
matches determines the drip amount. If a required mechanism is enabled but the credential is missing
or invalid, the request is rejected.

## Mechanism priority

| Priority | Mechanism         | Drip amount                                    | Requirement                                               |
|----------|-------------------|------------------------------------------------|-----------------------------------------------------------|
| 1        | **API key**       | Per-key override, or `drip.anonymous`          | `Authorization: Bearer pour_key_…` header                 |
| 2        | **Signed wallet** | `drip.signed` (falls back to `drip.anonymous`) | ADR-036 signature over a server-issued nonce              |
| 3        | **Proof-of-work** | `drip.anonymous`                               | Valid Altcha solution                                     |
| 4        | **Anonymous**     | `drip.anonymous`                               | No credential — allowed only when all others are disabled |

The **per-address daily cap** (`drip.max_per_address_per_day`) is always enforced, regardless of
mechanism. The same public key is recognised across all bech32 prefixes (cosmos1…, osmo1…, etc.),
so switching prefixes cannot bypass the cap.

---

## IP rate limiting

```yaml
abuse:
  ip_rate_limit:
    requests_per_window: 10
    window: 1h
```

Applied to every request before mechanism evaluation. A `429 Too Many Requests` response includes
a `Retry-After` header indicating seconds until the window resets.

---

## API keys

Intended for programmatic access — CI pipelines, scripts, and dev tools. Not shown in the web UI.

### Enable

```yaml
abuse:
  api_keys:
    enabled: true
```

### Issue a key

```sh
TOKEN=$(cat .pour-admin-token)

curl -X POST http://localhost:8080/admin/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "label": "ci-bot",
    "chain_scope": ["osmosis-1"],
    "per_chain_drips": {"osmosis-1": "3000000uosmo"},
    "rate_limit_per_hour": 100
  }'
```

The response contains the `secret` field — this is the bearer token. It is returned **once only**;
store it securely.

```json
{
  "id": "01JXYZ",
  "secret": "pour_key_abc123",
  "label": "ci-bot"
}
```

### Use the key

```sh
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer pour_key_abc123' \
  -d '{"chain_id":"osmosis-1","address":"osmo1..."}'
```

### Key fields

| Field                 | Description                                                                              |
|-----------------------|------------------------------------------------------------------------------------------|
| `label`               | Human-readable name for the key.                                                         |
| `chain_scope`         | List of chain IDs this key may drip on. Use `["*"]` for all chains.                      |
| `per_chain_drips`     | Per-chain drip override (coin string). Inherits `drip.anonymous` when absent.            |
| `rate_limit_per_hour` | Max drips per hour for this key. `0` = no per-key limit (global IP limit still applies). |
| `expires_at`          | Optional expiry (RFC3339). Omit for a non-expiring key.                                  |

### Manage keys

```sh
# List active keys
curl http://localhost:8080/admin/api-keys \
  -H "Authorization: Bearer $TOKEN"

# Revoke a key
curl -X DELETE http://localhost:8080/admin/api-keys/01JXYZ \
  -H "Authorization: Bearer $TOKEN"
```

---

## Signed wallet challenge

The signer fetches a one-time nonce, signs it using ADR-036 arbitrary-message format (supported by
Keplr and most Cosmos wallets), then includes the signature in the pour request.

### Enable

```yaml
abuse:
  signature_challenge:
    enabled: true
    require_predicate: none   # none | has_balance
```

### Optional predicate

When `require_predicate: has_balance`, the server also verifies that the signer holds a minimum
balance on-chain before granting the higher drip amount:

```yaml
signature_challenge:
  enabled: true
  require_predicate: has_balance
  predicate_chain_id: cosmoshub-4     # chain to query; defaults to the chain being dripped
  predicate_min_amount: "1000000uatom"
```

| `require_predicate` | What is checked                                                   |
|---------------------|-------------------------------------------------------------------|
| `none` *(default)*  | Signature only — no chain query                                   |
| `has_balance`       | Signer's balance on `predicate_chain_id` ≥ `predicate_min_amount` |

`predicate_chain_id` defaults to the chain being dripped. Set it to e.g. `cosmoshub-4` to require
ATOM holders regardless of which testnet they request from. The predicate chain must be present in
`chains.yml` — pour uses its existing gRPC/REST client to query balances and does not open new
connections on the fly. Chains used only as predicate sources should have `enabled: false`.

### Flow

```sh
# 1. Fetch a nonce (valid 5 minutes)
NONCE=$(curl -s http://localhost:8080/v1/sign/nonce | jq -r .nonce)

# 2. Sign with your wallet (Keplr, keplr-extension, or CLI)
# The signed payload follows ADR-036: amino JSON over the nonce string.

# 3. Include the signature in the pour request
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -d "{
    \"chain_id\": \"osmosis-1\",
    \"address\": \"osmo1...\",
    \"signature\": {
      \"nonce\": \"$NONCE\",
      \"address\": \"osmo1...\",
      \"pubkey\": \"<base64-compressed-pubkey>\",
      \"signature\": \"<base64-signature>\"
    }
  }"
```

---

## Proof-of-work (Altcha)

The embedded web UI handles PoW automatically using the Altcha widget. For direct API use:

### Enable

```yaml
abuse:
  pow:
    enabled: true
    difficulty: medium   # easy | medium | hard | <positive integer>
```

`difficulty` maps to Altcha's `maxNumber` parameter:

| Value            | `maxNumber`   |
|------------------|---------------|
| `easy`           | 50 000        |
| `medium`         | 100 000       |
| `hard`           | 200 000       |
| positive integer | used directly |

### Flow

```sh
# 1. Fetch a challenge
CHALLENGE=$(curl -s http://localhost:8080/v1/pow/challenge | jq -r .challenge)

# 2. Solve it client-side using the Altcha JS library or SDK

# 3. Include the solved credential in the pour request
curl -X POST http://localhost:8080/v1/pour \
  -H 'Content-Type: application/json' \
  -d "{
    \"chain_id\": \"osmosis-1\",
    \"address\": \"osmo1...\",
    \"pow\": {
      \"challenge\": \"$CHALLENGE\",
      \"solution\": \"<altcha-solution-string>\"
    }
  }"
```