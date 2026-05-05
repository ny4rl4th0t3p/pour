#!/bin/sh
# Initialises a single-validator simd devnet with the standard test mnemonic
# pre-funded, then starts the node. Runs inside the chain container.
set -e

CHAIN_ID=test-1
MONIKER=test-node
DENOM=stake
KEY_NAME=faucet
# Standard cosmjs/keplr all-zeros entropy test mnemonic.
# Derives cosmos19rl4cm2hmr8afy4kldpxz3fka4jguq0auqdal4 at m/44'/118'/0'/0/0.
MNEMONIC="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

SIMD_HOME="/simd"
STATE_DIR="/state"

simd init "$MONIKER" --chain-id "$CHAIN_ID" --home "$SIMD_HOME"

printf '%s\n' "$MNEMONIC" | \
  simd keys add "$KEY_NAME" --recover --keyring-backend test --home "$SIMD_HOME"

FAUCET_ADDR=$(simd keys show "$KEY_NAME" -a --keyring-backend test --home "$SIMD_HOME")

mkdir -p "$STATE_DIR"

# Derive distributor at index 1 — pour uses this key to send drips.
# Expose address so the smoke script can poll for the refill from the holder.
printf '%s\n' "$MNEMONIC" | \
  simd keys add distributor --recover --keyring-backend test --home "$SIMD_HOME" --index 1
DISTRIBUTOR_ADDR=$(simd keys show distributor -a --keyring-backend test --home "$SIMD_HOME")
printf '%s' "$DISTRIBUTOR_ADDR" > "$STATE_DIR/distributor_addr"

# Derive recipient at index 2 (no genesis funding, not a distributor).
printf '%s\n' "$MNEMONIC" | \
  simd keys add recipient --recover --keyring-backend test --home "$SIMD_HOME" --index 2
RECIPIENT_ADDR=$(simd keys show recipient -a --keyring-backend test --home "$SIMD_HOME")
printf '%s' "$RECIPIENT_ADDR" > "$STATE_DIR/recipient_addr"

simd genesis add-genesis-account "$FAUCET_ADDR" "10000000000${DENOM}" --home "$SIMD_HOME"
simd genesis gentx "$KEY_NAME" "1000000${DENOM}" \
  --chain-id "$CHAIN_ID" --keyring-backend test --home "$SIMD_HOME"
simd genesis collect-gentxs --home "$SIMD_HOME"

exec simd start \
  --home "$SIMD_HOME" \
  --grpc.enable true \
  --grpc.address 0.0.0.0:9090 \
  --api.enable \
  --api.address tcp://0.0.0.0:1317 \
  --minimum-gas-prices "0${DENOM}" \
  --log_level error