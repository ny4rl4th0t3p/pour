# E2E Test Catalogue

All tests run against real Docker containers (ibc-go-simd v8.5.2 + Hermes relayer).
The binary under test is provided via `POUR_BIN` or `POUR_VERSION`.

---

## Infrastructure legend

| Component          | Role                                                                                     |
|--------------------|------------------------------------------------------------------------------------------|
| **hub-1**          | IBC source-only chain — chain ID `hub-1`, denom `stake`, bech32 `cosmos`; no native drip |
| **mynet-1**        | operator's chain — chain ID `mynet-1`, denom `uosmo`, bech32 `cosmos`                    |
| **Mock registry**  | httptest server serving `chain.json` + `_IBC/*.json`                                     |
| **Hermes relayer** | relays IBC packets between hub-1 and mynet-1                                             |
| **pour**           | binary under test; config written to a temp dir before each test                         |

The `pour-faucet` account (`TestMnemonic` at index 0) is seeded in genesis on every
chain that starts. mynet-1 genesis includes both `stake` and `uosmo` for that account.

---

## IBC discovery

### TestIBCDiscovery

**What it tests:** pour fetches IBC channel data from the chain registry, exposes it
correctly in the API, and surfaces the `ibc_drips` config for IBC-only destination chains.

**Infrastructure:** hub-1 + mock registry (mynet-1 placeholder only, not running).

```mermaid
sequenceDiagram
    participant T as test
    participant R as Mock registry
    participant P as pour daemon
    participant A as hub-1
    P ->> R: fetch /hub/chain.json
    P ->> R: fetch /mynet/chain.json (placeholder)
    P ->> R: fetch /_IBC/hub-mynet.json
    P ->> A: query IBC channel status (channel-0)
    T ->> P: GET /v1/chains/hub-1
    P -->> T: IBCChannels[channel-0, transfer, live]
    T ->> P: GET /v1/info
    P -->> T: ibc_channel_count: 1
    T ->> P: GET /v1/chains/mynet-1
    P -->> T: IBCDrips[{source: hub-1, denom: stake}]
```

**Assertions:**

- `GET /v1/chains/hub-1` returns exactly one IBC channel (`channel-0`, port `transfer`, peer `mynet`, status `live`,
  preferred)
- `GET /v1/info` returns `ibc_channel_count: 1`
- `GET /v1/chains/mynet-1` returns one IBC drip entry with `source_chain_id: hub-1`, `denom: stake`

---

## Auto mode

### TestAutoMode_HappyPath

**What it tests:** `pour serve --auto` parses genesis from a bind-mounted home dir,
self-funds the faucet address using the fund mnemonic, and serves a native drip.

**Infrastructure:** mynet-1 only (no registry).

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon (--auto)
    participant A as mynet-1
    P ->> A: read genesis.json (bind mount)
    P ->> A: MsgSend self-fund (POUR_FUND_MNEMONIC → pour address)
    P ->> P: ready
    T ->> P: POST /v1/pour {chain_id: mynet-1, address: TestAutoRecipient}
    P ->> A: MsgSend (stake → TestAutoRecipient)
    A -->> P: tx confirmed
    P -->> T: {status: confirmed, tx_hash}
    T ->> A: WaitForBalance(TestAutoRecipient, stake ≥ 1)
```

**Assertions:**

- Response `status == "confirmed"`, `tx_hash` non-empty
- `TestAutoRecipient` balance ≥ 1 stake on mynet-1

---

### TestAutoMode_WaitForFunding

**What it tests:** when no fund mnemonic is provided, pour polls until an external actor
funds its address, then begins serving requests.

**Infrastructure:** mynet-1 only. Pour address is derived from `RelayerMnemonic` (not
seeded in genesis).

```mermaid
sequenceDiagram
    participant T as test
    participant G as goroutine (3 s delay)
    participant P as pour daemon (--auto)
    participant A as mynet-1
    P ->> A: poll balance (RelayerAddr) — waiting...
    G ->> G: sleep 3s
    G ->> A: MsgSend validator→RelayerAddr (5000000stake)
    A -->> P: balance detected
    P ->> P: ready
    T ->> P: POST /v1/pour {chain_id: mynet-1, address: TestAutoRecipient}
    P ->> A: MsgSend (stake → TestAutoRecipient)
    A -->> P: tx confirmed
    P -->> T: {status: confirmed, tx_hash}
```

**Assertions:**

- `StartPourAuto` returns only after pour has detected its balance and is healthy
- Response `status == "confirmed"`, `tx_hash` non-empty

---

### TestAutoMode_HotReload

**What it tests:** pour detects a devnet chain reset (block height regression),
reconnects the gRPC client, and resumes serving drips without operator intervention.

**Infrastructure:** mynet-1 in restartable mode (simd loops; `ResetChain` kills simd
and waits for it to restart from block 1).

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon (--auto)
    participant A as mynet-1 (restartable)
    T ->> P: POST /v1/pour (baseline)
    P ->> A: MsgSend
    A -->> P: confirmed
    P -->> T: {status: confirmed}
    T ->> A: WaitForBlockHeight(15)
    T ->> A: ResetChain → height resets to 0
    P ->> P: watcher detects height regression
    P ->> A: reconnect gRPC client

    loop retry up to 30s
        T ->> P: POST /v1/pour
        P -->> T: {status: confirmed}
    end
```

**Assertions:**

- Baseline drip before reset: `status == "confirmed"`
- After reset: at least one drip succeeds within 30 s with `status == "confirmed"`

---

### TestAutoMode_GRPCToRESTFailover

**What it tests:** pour automatically switches from gRPC to REST when the active gRPC
endpoint goes down mid-session.

**Infrastructure:** mynet-1 + a local TCP proxy in front of its gRPC port.

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon (--auto)
    participant X as TCP proxy (gRPC)
    participant A as mynet-1
    T ->> P: POST /v1/pour (baseline)
    P ->> X: gRPC (proxied)
    X ->> A: gRPC
    A -->> P: confirmed
    P -->> T: {status: confirmed}
    T ->> X: proxy.Close()
    T ->> P: POST /v1/pour (failover)
    P ->> A: REST (direct)
    A -->> P: confirmed
    P -->> T: {status: confirmed}
```

**Assertions:**

- Baseline drip (gRPC path): `status == "confirmed"`
- Post-failover drip (REST path): `status == "confirmed"`

---

### TestAutoMode_RESTOnly

**What it tests:** pour works end-to-end with only a REST/LCD endpoint — no gRPC
configured. All wire operations use REST.

**Infrastructure:** mynet-1 only; `--grpc ""` omits gRPC from the auto config.

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon (--auto, REST only)
    participant A as mynet-1
    P ->> A: MsgSend self-fund via REST
    P ->> P: ready
    T ->> P: POST /v1/pour {chain_id: mynet-1, address: TestAutoRecipient}
    P ->> A: MsgSend drip via REST
    A -->> P: confirmed
    P -->> T: {status: confirmed, tx_hash}
    T ->> A: WaitForBalance(TestAutoRecipient, stake ≥ 1)
```

**Assertions:**

- Response `status == "confirmed"`, `tx_hash` non-empty
- `TestAutoRecipient` balance ≥ 1 stake on mynet-1

---

## IBC transfers

### TestIBCTransfer_HappyPath

**What it tests:** full IBC drip path — a request with `denom=stake` on mynet-1 causes
pour to send `MsgTransfer` from hub-1; the recipient on mynet-1 receives the IBC voucher.

**Infrastructure:** hub-1 + mynet-1 + Hermes relayer + mock registry.
mynet-1 is IBC-only (no native wallet).

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon
    participant A as hub-1
    participant H as Hermes relayer
    participant B as mynet-1
    T ->> P: POST /v1/pour {chain_id: mynet-1, address, denom: stake}
    P ->> P: findIBCDrip(stake) → source: hub-1
    P ->> A: MsgTransfer (stake → B recipient, channel-0)
    A -->> P: tx confirmed
    P -->> T: {status: confirmed, tx_hash}
    A ->> H: IBC packet
    H ->> B: relay packet
    T ->> B: WaitForBalance(recipient, ibc/... ≥ 1_000_000)
```

**Assertions:**

- Response `status == "confirmed"`, `tx_hash` non-empty
- mynet-1 recipient holds ≥ 1,000,000 of `ibc/SHA256(transfer/channel-0/stake)`

---

### TestIBCTransfer_NativeOnDestination

**What it tests:** a chain configured with both native and IBC drips issues a native
`MsgSend` from its own wallet when no denom is specified.

**Infrastructure:** hub-1 + mynet-1 + mock registry. No relayer needed.
mynet-1 has `DualDripDestination: true` (native drip + IBC drip, `batch_window: "0s"`).
hub-1 is startup-only — pour connects to it but no test messages flow through it.

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon
    participant B as mynet-1
    T ->> P: POST /v1/pour {chain_id: mynet-1, address}
    P ->> P: denom="" + native drip exists → native path
    P ->> B: MsgSend (stake, KeyIndex=0)
    B -->> P: tx confirmed
    P -->> T: {status: confirmed, tx_hash}
    T ->> B: WaitForBalance(recipient, stake ≥ 1_000_000)
```

**Assertions:**

- Response `status == "confirmed"`, `tx_hash` non-empty
- mynet-1 recipient holds ≥ 1,000,000 native `stake` (not an IBC voucher)

---

### TestIBCTransfer_NativeAndIBC

**What it tests:** both native and IBC drip paths work independently on the same
destination chain within the same pour session.

**Infrastructure:** hub-1 + mynet-1 + Hermes relayer + mock registry.
mynet-1 has `DualDripDestination: true`.

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon
    participant A as hub-1
    participant H as Hermes relayer
    participant B as mynet-1
    T ->> P: POST /v1/pour {chain_id: mynet-1, address}
    P ->> B: MsgSend (stake, native path)
    B -->> P: confirmed
    P -->> T: {status: confirmed}
    T ->> P: POST /v1/pour {chain_id: mynet-1, address, denom: stake}
    P ->> A: MsgTransfer (stake → B recipient, IBC path)
    A -->> P: confirmed
    P -->> T: {status: confirmed}
    A ->> H: IBC packet
    H ->> B: relay packet
    T ->> B: WaitForBalance(recipient, stake ≥ 1_000_000)
    T ->> B: WaitForBalance(recipient, ibc/... ≥ 1_000_000)
```

**Assertions:**

- Native pour: `status == "confirmed"`, `tx_hash` non-empty
- IBC pour: `status == "confirmed"`, `tx_hash` non-empty
- mynet-1 recipient holds ≥ 1,000,000 native `stake`
- mynet-1 recipient holds ≥ 1,000,000 `ibc/SHA256(transfer/channel-0/stake)`

---

### TestIBCTransfer_SourceChainRejectsDirect

**What it tests:** hub-1, configured as an IBC source-only chain (no `drip.anonymous`, no
`ibc.drips`), rejects direct pour requests with HTTP 400 — both the native drip path (no
denom) and any denom request. hub-1 must not be usable as a public faucet.

**Infrastructure:** hub-1 + mock registry (mynet-1 placeholder only, not running).

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon

    T->>P: POST /v1/pour {chain_id: hub-1, address}
    P->>P: snap.Drip.Anonymous == "" → no native drip
    P-->>T: HTTP 400

    T->>P: POST /v1/pour {chain_id: hub-1, address, denom: stake}
    P->>P: findIBCDrip("stake") → no ibc.drips on hub-1
    P-->>T: HTTP 400
```

**Assertions:**

- Native pour to hub-1: HTTP `400`
- IBC-denom pour to hub-1 with `denom: stake`: HTTP `400`
- No transaction broadcast on any chain

---

### TestIBCTransfer_UnknownDenom

**What it tests:** requesting a denom that has no matching IBC drip config returns
HTTP 400 without broadcasting any transaction.

**Infrastructure:** mock registry + hub-1 (startup-only — pour requires a live endpoint to become healthy; no
transactions are sent to it).
mynet-1 is IBC-only with a single `stake` drip configured; it is not running.

```mermaid
sequenceDiagram
    participant T as test
    participant P as pour daemon
    T ->> P: POST /v1/pour {chain_id: mynet-1, address, denom: notconfigured}
    P ->> P: findIBCDrip("notconfigured") → not found
    P -->> T: HTTP 400
```

**Assertions:**

- HTTP response status `400`
- No transaction broadcast on any chain