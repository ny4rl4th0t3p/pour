# Transports — gRPC & REST

Pour can communicate with chain nodes via two transports: **gRPC** (default) and **REST** (LCD).
Both support the same five wire operations and behave identically from the faucet's perspective.

## Wire operations

Every drip request resolves to exactly five remote calls:

| Operation     | gRPC                                    | REST                                                |
|---------------|-----------------------------------------|-----------------------------------------------------|
| Query account | `cosmos.auth.v1beta1.Query/Account`     | `GET /cosmos/auth/v1beta1/accounts/{address}`       |
| Simulate      | `cosmos.tx.v1beta1.Service/Simulate`    | `POST /cosmos/tx/v1beta1/simulate`                  |
| Broadcast tx  | `cosmos.tx.v1beta1.Service/BroadcastTx` | `POST /cosmos/tx/v1beta1/txs`                       |
| Get tx        | `cosmos.tx.v1beta1.Service/GetTx`       | `GET /cosmos/tx/v1beta1/txs/{hash}`                 |
| Query balance | `cosmos.bank.v1beta1.Query/Balance`     | `GET /cosmos/bank/v1beta1/balances/{addr}/by_denom` |

## Configuring endpoints

```yaml
endpoints:
  grpc:
    - grpc.osmosis.zone:443         # no scheme; plain or TLS auto-detected
    - grpc-fallback.osmosis.zone:443
  rest:
    - https://lcd.osmosis.zone      # scheme required
    - https://lcd-fallback.osmosis.zone
```

Multiple endpoints of the same type form a **pool**. Pour rotates through them round-robin, marking
endpoints unhealthy when they fail and skipping them until they recover.

## Transport selection and failover

Pour selects the initial transport at startup:

1. If `endpoints.grpc` is non-empty → start with gRPC
2. Else if `endpoints.rest` is non-empty → start with REST
3. Both empty → error at startup

At runtime, when an endpoint returns an unavailable error (connection refused, gRPC `UNAVAILABLE`,
5xx from REST, or timeout):

```
gRPC endpoint A fails
  → try gRPC endpoint B
  → gRPC pool exhausted
  → switch to REST endpoint A
  → try REST endpoint B
  → REST pool exhausted
  → ErrNoEndpointAvailable
```

The switch to REST is transparent — signing, fee estimation, and sequence management are unchanged.

!!! note
Simulation (`Simulate`) returns `(0, nil)` on REST when the node returns a non-2xx response.
Pour treats this as "simulation unavailable" and falls back to the configured gas price without
simulation, the same behaviour as when simulation is not supported on a gRPC endpoint.

## Endpoint health probing

Pour runs a background probe loop for each endpoint pool. Unhealthy endpoints are re-probed
periodically; once they respond successfully they re-enter the rotation.

## REST-only chains

Chains that expose only a REST/LCD endpoint (no gRPC) work end-to-end:

```yaml
endpoints:
  rest:
    - http://localhost:1317
```

Fee estimation, sequence management, broadcast, and confirmation polling all operate over REST.

## gRPC TLS

Pour auto-detects TLS for gRPC endpoints on port 443. For non-standard TLS ports, no additional
configuration is needed — the endpoint URL is used as-is and TLS is negotiated based on the port
and server capabilities.

## Checking the active transport

The chain status endpoint shows which transport is currently in use:

```sh
curl -s http://localhost:8080/admin/chains/mychain-1/status \
  -H "Authorization: Bearer $(cat .pour-admin-token)"
```