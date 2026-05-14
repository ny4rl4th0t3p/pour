# IBC drips

Pour can drip tokens to recipients on any IBC-connected chain via `MsgTransfer`. The
source chain holds the tokens; pour sends them to the recipient address on the destination
chain as an IBC voucher.

## How it works

```
Requester → POST /v1/pour {"chain_id": "mynet-1", "address": "cosmos1...", "denom": "uatom"}
               ↓
Pour finds the IBC drip config for denom "uatom" on mynet-1 → source: cosmoshub-4
               ↓
MsgTransfer on cosmoshub-4: uatom escrowed → cosmos1... via channel-X
               ↓
Relayer delivers packet: cosmos1... receives ibc/.../uatom on mynet-1
               ↓
Response: {"status": "confirmed", "tx_hash": "..."}
```

Pour's wallet holds native tokens on the source chain. The `MsgTransfer` escrows them
there; the relayer delivers the packet and the destination chain mints the IBC voucher for
the recipient.

The pour response is returned after the `MsgTransfer` is confirmed on the **source** chain,
not after the IBC packet is relayed to the destination.

## Configuration

IBC drips are declared as a list under `ibc.drips` on the destination chain. Each entry
names a source chain, a drip amount (in the source chain's denom), and a per-address daily
cap. The source chain must be present in the config with a funded wallet and working
endpoints. No `drip` block is required on the source chain — omitting it makes it
source-only, meaning it broadcasts `MsgTransfer` for other chains but does not serve native
drips itself.

```yaml
chains:
  - chain_id: cosmoshub-4          # source chain — holds the tokens, fires MsgTransfer
    enabled: true                  # no drip block: source-only, not a public ATOM faucet

  - chain_id: mynet-1              # destination chain
    enabled: true
    ibc:
      timeout: 10m
      drips:
        - source_chain_id: cosmoshub-4
          anonymous: "1000000uatom"         # denom on the source chain
          max_per_address_per_day: "10000000uatom"
```

Requesting a drip on `mynet-1` requires an explicit `denom` field:

```sh
curl -X POST http://localhost:8080/v1/pour \
  -d '{"chain_id":"mynet-1","address":"cosmos1...","denom":"uatom"}'
```

## Native and IBC drips on the same chain

A chain can have both a native drip (`drip.anonymous`) and one or more IBC drip entries
simultaneously. The `denom` field in the pour request selects which path to use:

- `denom` omitted → native drip (`MsgSend` from the chain's own wallet)
- `denom` present → IBC drip whose `anonymous` amount matches that denom

```yaml
chains:
  - chain_id: cosmoshub-4
    enabled: true                  # source-only: no drip block, not a public ATOM faucet

  - chain_id: mynet-1
    enabled: true
    endpoints:
      grpc:
        - localhost:9090
    drip:
      anonymous: "1000000umynet"             # native drip — no denom in request
      max_per_address_per_day: "10000000umynet"
    ibc:
      timeout: 10m
      drips:
        - source_chain_id: cosmoshub-4
          anonymous: "1000000uatom"          # IBC drip — denom: uatom in request
          max_per_address_per_day: "10000000uatom"
```

## Multiple IBC drips

A destination chain can accept IBC drips from multiple source chains:

```yaml
  - chain_id: mynet-1
    enabled: true
    ibc:
      timeout: 10m
      drips:
        - source_chain_id: cosmoshub-4
          anonymous: "1000000uatom"
          max_per_address_per_day: "10000000uatom"
        - source_chain_id: osmosis-1
          anonymous: "1000000uosmo"
          max_per_address_per_day: "10000000uosmo"
```

## Channel selection

Pour discovers IBC channels from the chain registry. For each source→destination pair it:

1. Prefers channels with `status: live` and `preferred: true`
2. Falls back to any `status: live` channel
3. Returns `503` if no live channel exists

You can inspect the available channels:

```sh
curl -s http://localhost:8080/v1/chains/mynet-1 | jq .ibc_channels
```

```json
[
  {
    "peer_chain_name": "cosmoshub",
    "channel_id": "channel-0",
    "peer_channel_id": "channel-141",
    "port_id": "transfer",
    "status": "live",
    "preferred": true
  }
]
```

## Transfer timeout

The `ibc.timeout` value sets the `TimeoutTimestamp` on the `MsgTransfer`. If the packet is
not relayed and acknowledged within this window, the transfer times out and the tokens are
returned to the faucet address on the source chain.

```yaml
ibc:
  timeout: 30m   # increase for slow relayers
  drips:
    - source_chain_id: cosmoshub-4
      anonymous: "1000000uatom"
      max_per_address_per_day: "10000000uatom"
```

## Response codes

| Status | Meaning                                                            |
|--------|--------------------------------------------------------------------|
| 200    | `MsgTransfer` confirmed on the source chain                        |
| 400    | No IBC drip configured for the requested denom                     |
| 502    | IBC transfer failed (broadcast error on the source chain)          |
| 503    | Source chain not active, or no live IBC channel to the destination |