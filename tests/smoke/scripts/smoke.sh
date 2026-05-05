#!/bin/sh
# End-to-end smoke test for pour. Runs inside the smoke container.
# Polls until the faucet is ready, then verifies health and a successful drip.
set -e

BASE="http://faucet:8080"
CHAIN_REST="http://chain:1317"
FAUCET_ADDR="cosmos19rl4cm2hmr8afy4kldpxz3fka4jguq0auqdal4"
DRIP="1000000stake"

# Returns the stake balance (integer) for the given address, or empty on error.
# Strips spaces from JSON before matching to handle "amount": "123" and "amount":"123".
stake_balance() {
  curl -sf "$CHAIN_REST/cosmos/bank/v1beta1/balances/$1" \
    | tr -d ' ' \
    | grep -o '"amount":"[0-9]*"' \
    | head -1 \
    | grep -o '[0-9]*' \
    || true
}

echo "==> waiting for addresses from chain init"
i=0
while [ "$i" -lt 60 ]; do
  [ -f /state/recipient_addr ] && [ -f /state/distributor_addr ] && break
  i=$((i + 1))
  [ "$i" -eq 60 ] && { echo "TIMEOUT: address files not written by chain init"; exit 1; }
  sleep 1
done
DISTRIBUTOR=$(cat /state/distributor_addr)
RECIPIENT=$(cat /state/recipient_addr)
echo "    distributor: $DISTRIBUTOR"
echo "    recipient:   $RECIPIENT"
if [ "$RECIPIENT" = "$FAUCET_ADDR" ] || [ "$RECIPIENT" = "$DISTRIBUTOR" ]; then
  echo "FAIL: recipient address collides with faucet or distributor"
  exit 1
fi

echo "==> waiting for faucet"
i=0
while [ "$i" -lt 60 ]; do
  curl -sf "$BASE/health" > /dev/null 2>&1 && break
  i=$((i + 1))
  [ "$i" -eq 60 ] && { echo "TIMEOUT: faucet did not become ready"; exit 1; }
  sleep 2
done

echo "==> health"
curl -sf "$BASE/health" | grep -q '"status":"ok"'

echo "==> waiting for chain REST API"
i=0
while [ "$i" -lt 30 ]; do
  curl -sf "$CHAIN_REST/cosmos/bank/v1beta1/balances/$FAUCET_ADDR" > /dev/null 2>&1 && break
  i=$((i + 1))
  [ "$i" -eq 30 ] && { echo "TIMEOUT: chain REST API not ready"; exit 1; }
  sleep 2
done

echo "==> faucet funded"
FAUCET_BAL=$(stake_balance "$FAUCET_ADDR")
echo "    faucet: ${FAUCET_BAL}stake"
if [ -z "$FAUCET_BAL" ] || [ "$FAUCET_BAL" -le 1000000 ]; then
  echo "FAIL: faucet balance insufficient (${FAUCET_BAL}stake)"
  exit 1
fi

echo "==> waiting for distributor refill (refill loop seeds distributor from holder at startup)"
i=0
while [ "$i" -lt 30 ]; do
  DIST_BAL=$(stake_balance "$DISTRIBUTOR")
  [ -n "$DIST_BAL" ] && [ "$DIST_BAL" -gt 0 ] && break
  i=$((i + 1))
  [ "$i" -eq 30 ] && { echo "TIMEOUT: distributor balance never became nonzero"; exit 1; }
  sleep 2
done
echo "    distributor: ${DIST_BAL}stake"

echo "==> recipient balance (pre-pour)"
PRE_BAL=$(stake_balance "$RECIPIENT")
PRE_BAL=${PRE_BAL:-0}
echo "    recipient pre: ${PRE_BAL}stake"

echo "==> pour"
RESP=$(curl -sf -X POST "$BASE/v1/pour" \
  -H 'Content-Type: application/json' \
  -d "{\"chain_id\":\"test-1\",\"address\":\"$RECIPIENT\"}")
echo "    $RESP"
echo "$RESP" | grep -q '"tx_hash"'
echo "$RESP" | grep -q '"status":"confirmed"'
echo "$RESP" | grep -q "\"amount\":\"$DRIP\""

# The faucet waits for tx confirmation before returning, but the REST API
# may lag one block behind the confirmed state. Give it a moment.
sleep 3

echo "==> recipient balance (post-pour)"
POST_BAL=$(stake_balance "$RECIPIENT")
POST_BAL=${POST_BAL:-0}
echo "    recipient post: ${POST_BAL}stake"
EXPECTED_POST=$((PRE_BAL + 1000000))
if [ "$POST_BAL" -ne "$EXPECTED_POST" ]; then
  echo "FAIL: expected ${EXPECTED_POST}stake, got ${POST_BAL}stake"
  exit 1
fi

echo "==> /v1/info"
curl -sf "$BASE/v1/info" | grep -q '"version"'

echo "==> /v1/chains"
CHAINS=$(curl -sf "$BASE/v1/chains")
echo "    $CHAINS"
echo "$CHAINS" | grep -q '"chain_id":"test-1"'

echo "==> rate limit (second pour from same IP; limit=1 in smoke chains.yml)"
RESP2=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/v1/pour" \
  -H 'Content-Type: application/json' \
  -d "{\"chain_id\":\"test-1\",\"address\":\"$RECIPIENT\"}")
if [ "$RESP2" != "429" ]; then
  echo "FAIL: expected 429 on second pour from same IP, got $RESP2"
  exit 1
fi

echo "==> unknown chain returns 4xx"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/v1/pour" \
  -H 'Content-Type: application/json' \
  -d '{"chain_id":"doesnotexist-99","address":"cosmos1abc"}')
if [ "$STATUS" = "200" ]; then
  echo "FAIL: expected 4xx for unknown chain, got $STATUS"
  exit 1
fi

echo "==> passed"