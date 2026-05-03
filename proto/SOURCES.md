# Proto sources

The `.proto` files vendored in this directory are the narrow surface we depend on for
tx construction and account queries.

Two things are true simultaneously and should not be conflated:

- **The message wire formats are stable.** The encoded bytes for `MsgSend`, `Coin`, `BaseAccount`,
  `SignDoc`, `Tx`, and `MsgTransfer` have not changed since SDK v0.47. Vendoring from any version
  ≥ v0.47 produces identical bytes on the wire for the messages we use.

- **The proto files themselves have evolved.** Annotation-only files (`amino/amino.proto`,
  `cosmos/msg/v1/msg.proto`) were introduced in v0.47 and have continued to change. We do not
  claim the files are frozen — only that the wire formats of the messages we depend on are.

We vendor from current stable releases (SDK v0.54.2, ibc-go v11.0.0) for cosmetic alignment
with what is current, not because the version has a technical impact on our use case.

## Vendored revisions

| Source            | Revision | Commit                                   | Date       | Notes                                    |
|-------------------|----------|------------------------------------------|------------|------------------------------------------|
| cosmos/cosmos-sdk | v0.54.2  | d15e81507416a66a1b6d65c5209dce414d360121 | 2026-05-02 |                                          |
| cosmos/ibc-go     | v11.0.0  | 3e5f5e5bb153b6a126e67ccf45b565bf14d7ddd7 | 2026-05-02 |                                          |
| evmos/ethermint   | v0.22.0  | c239fb335cf9647aafd0a43f6fd7e241328c55a6 | 2026-05-02 | Last release before archival; Apache-2.0 |

## Local modifications

- `cosmos/base/abci/v1beta1/abci.proto`: removed `import "tendermint/abci/types.proto"` and
  changed `TxResponse.events` and `Result.events` from `repeated tendermint.abci.Event` to
  `repeated bytes`. The faucet does not use ABCI events; this keeps the generated code free
  of any cometbft/tendermint Go module dependency and avoids version coupling across chains.

- `cosmos/tx/v1beta1/service.proto`: removed `import "tendermint/types/block.proto"` and
  `import "tendermint/types/types.proto"`, changed `GetBlockWithTxsResponse.block_id` and
  `.block` fields to `bytes`. The faucet does not use block queries; the same rationale as above.

## How to bump

1. Copy the target `.proto` files from the upstream repo at the chosen revision.
2. Update the table above with the exact commit hash and date.
3. Run `make proto-gen` to regenerate bindings.
4. Run `make proto-lint` to verify no regressions.
5. Open a PR with the `.proto` changes and updated SOURCES.md — do not auto-merge.

## Vendored proto files

From cosmos-sdk:

- `amino/amino.proto`
- `cosmos/auth/v1beta1/auth.proto`, `query.proto`
- `cosmos/bank/v1beta1/bank.proto`, `tx.proto`
- `cosmos/base/abci/v1beta1/abci.proto` *(modified — see Local modifications)*
- `cosmos/base/query/v1beta1/pagination.proto`
- `cosmos/base/v1beta1/coin.proto`
- `cosmos/crypto/multisig/v1beta1/multisig.proto`
- `cosmos/crypto/secp256k1/keys.proto`
- `cosmos/msg/v1/msg.proto`
- `cosmos/query/v1/query.proto`
- `cosmos/tx/signing/v1beta1/signing.proto`
- `cosmos/tx/v1beta1/service.proto` *(modified — see Local modifications)*
- `cosmos/tx/v1beta1/tx.proto`

From ibc-go:

- `ibc/applications/transfer/v1/transfer.proto`
- `ibc/applications/transfer/v1/tx.proto`
- `ibc/core/client/v1/client.proto`

From ethermint:

- `ethermint/crypto/v1/ethsecp256k1/keys.proto`
- `ethermint/types/v1/account.proto`