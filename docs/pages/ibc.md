# IBC drips

Pour can drip to chains it holds no native tokens on by using IBC `MsgTransfer`. The source chain
holds the tokens; pour sends them to the destination chain's address via an ICS-20 transfer.

## How it works

```
Requester → POST /v1/pour {"chain_id": "network-1", "address": "cosmos1..."}
               ↓
Pour queries IBC channels between osmosis-1 and network-1
               ↓
MsgTransfer on osmosis-1: 10 uosmo escrowed on osmosis-1 → cosmos1... via channel-X
               ↓
Relayer delivers packet: cosmos1... receives ibc/.../uosmo on network-1
               ↓
Response: {"status": "confirmed", "tx_hash": "..."}
```

Pour's wallet holds native tokens on the source chain. The `MsgTransfer` escrows them there; the
relayer delivers the packet and the destination chain mints the IBC voucher for the recipient.

The pour response is returned after the `MsgTransfer` is confirmed on the **source** chain, not
after the IBC packet is relayed to the destination.

## Configuration

### Source chain

The source chain must have a funded faucet wallet, a working endpoint, and the IBC denom on its
balance. Configure it normally:

```yaml
chains:
  - chain_id: osmosis-1
    enabled: true
    endpoints:
      grpc:
        - grpc.osmosis.zone:443
    drip:
      anonymous: "1000000uosmo"
      max_per_address_per_day: "50000000uosmo"
```

### Destination chain

The destination chain needs only drip policy and an IBC stanza. It has no endpoints of its own.
The drip denom is the token as it exists on the **source chain** — that is what gets sent in the
`MsgTransfer`. Recipients on the destination chain receive the corresponding IBC voucher.

```yaml
chains:
  - chain_id: network-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"   # denom on the source chain (osmosis-1)
      max_per_address_per_day: "10000000uosmo"
    ibc:
      source_chain_id: osmosis-1  # tokens are sent from this chain
      timeout: 10m                # MsgTransfer timeout (default 10m)
```

!!! tip
Chains used only as predicate sources for the signed-wallet mechanism should have
`enabled: false` — enabling them opens a drip endpoint for a chain that has no funds.

## Channel selection

Pour discovers IBC channels from the chain registry. For each source→destination pair it:

1. Prefers channels with `status: live` and `preferred: true`
2. Falls back to any `status: live` channel
3. Returns `503` if no live channel exists

You can inspect the available channels:

```sh
curl -s http://localhost:8080/v1/chains/cosmoshub-4 | jq .ibc_channels
```

```json
[
  {
    "peer_chain_name": "osmosis",
    "channel_id": "channel-141",
    "peer_channel_id": "channel-0",
    "port_id": "transfer",
    "status": "live",
    "preferred": true
  }
]
```

## Transfer timeout

The `ibc.timeout` value sets the `TimeoutTimestamp` on the `MsgTransfer`. If the packet is not
relayed and acknowledged within this window, the transfer times out and the tokens are returned to
the faucet address on the source chain.

Increase this value for slow relayers:

```yaml
ibc:
  source_chain_id: osmosis-1
  timeout: 30m
```

## Multiple IBC destinations

A single source chain can serve many destination chains:

```yaml
chains:
  - chain_id: osmosis-1           # source
    enabled: true
    ...

  - chain_id: cosmoshub-4         # destination 1
    enabled: true
    drip:
      anonymous: "1000000uatom"
      max_per_address_per_day: "10000000uatom"
    ibc:
      source_chain_id: osmosis-1

  - chain_id: juno-1              # destination 2
    enabled: true
    drip:
      anonymous: "1000000ujuno"
      max_per_address_per_day: "10000000ujuno"
    ibc:
      source_chain_id: osmosis-1
```

## Response codes

| Status | Meaning                                                            |
|--------|--------------------------------------------------------------------|
| 200    | `MsgTransfer` confirmed on the source chain                        |
| 502    | IBC transfer failed (broadcast error on the source chain)          |
| 503    | Source chain not active, or no live IBC channel to the destination |