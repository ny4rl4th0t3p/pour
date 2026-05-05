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
echo "$RESP" | grep -q '"status"'
echo "$RESP" | grep -q '"drip_id"'
echo "$RESP" | grep -q "\"amount\":\"$DRIP\""

# Wait for the drip to settle: batch window fires (≤5s) then tx confirms.
EXPECTED_POST=$((PRE_BAL + 1000000))
echo "==> waiting for recipient balance to update after pour"
i=0
while [ "$i" -lt 30 ]; do
  POST_BAL=$(stake_balance "$RECIPIENT")
  POST_BAL=${POST_BAL:-0}
  [ "$POST_BAL" -eq "$EXPECTED_POST" ] && break
  i=$((i + 1))
  [ "$i" -eq 30 ] && { echo "TIMEOUT: balance never reached ${EXPECTED_POST}stake (last: ${POST_BAL}stake)"; exit 1; }
  sleep 1
done
echo "    recipient post: ${POST_BAL}stake"

echo "==> /v1/info"
curl -sf "$BASE/v1/info" | grep -q '"version"'

echo "==> /v1/chains"
CHAINS=$(curl -sf "$BASE/v1/chains")
echo "    $CHAINS"
echo "$CHAINS" | grep -q '"chain_id":"test-1"'

echo "==> batch: 2 concurrent pours (exercises batch window coalescing)"
BATCH_BASE=$POST_BAL
EXPECTED_BATCH=$((BATCH_BASE + 2000000))
curl -s -X POST "$BASE/v1/pour" \
  -H 'Content-Type: application/json' \
  -d "{\"chain_id\":\"test-1\",\"address\":\"$RECIPIENT\"}" \
  -o /tmp/bp1.json &
curl -s -X POST "$BASE/v1/pour" \
  -H 'Content-Type: application/json' \
  -d "{\"chain_id\":\"test-1\",\"address\":\"$RECIPIENT\"}" \
  -o /tmp/bp2.json &
wait
grep -q '"status"' /tmp/bp1.json || { echo "FAIL: batch pour 1 bad response"; cat /tmp/bp1.json; exit 1; }
grep -q '"status"' /tmp/bp2.json || { echo "FAIL: batch pour 2 bad response"; cat /tmp/bp2.json; exit 1; }
echo "==> waiting for batch pours to settle"
i=0
while [ "$i" -lt 30 ]; do
  BATCH_BAL=$(stake_balance "$RECIPIENT")
  BATCH_BAL=${BATCH_BAL:-0}
  [ "$BATCH_BAL" -eq "$EXPECTED_BATCH" ] && break
  i=$((i + 1))
  [ "$i" -eq 30 ] && { echo "TIMEOUT: balance never reached ${EXPECTED_BATCH}stake (last: ${BATCH_BAL}stake)"; exit 1; }
  sleep 1
done
echo "    balance after batch: ${BATCH_BAL}stake"

echo "==> rate limit (4th pour from same IP; limit=3 in smoke chains.yml)"
RESP2=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/v1/pour" \
  -H 'Content-Type: application/json' \
  -d "{\"chain_id\":\"test-1\",\"address\":\"$RECIPIENT\"}")
if [ "$RESP2" != "429" ]; then
  echo "FAIL: expected 429 on 4th pour from same IP, got $RESP2"
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

ADMIN="$BASE/admin"

echo "==> admin: distributor list"
DISTS=$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$ADMIN/distributors/test-1")
echo "    $DISTS"
echo "$DISTS" | grep -q '"index":1'
echo "$DISTS" | grep -q '"status":"healthy"'
echo "$DISTS" | grep -q '"address"'

echo "==> admin: chain status (healthy)"
CHAIN_ST=$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$ADMIN/chains/test-1/status")
echo "    $CHAIN_ST"
echo "$CHAIN_ST" | grep -q '"suspended":false'
echo "$CHAIN_ST" | grep -q '"multisend_disabled":false'

echo "==> admin: resume on non-suspended chain returns 409"
RESUME_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" "$ADMIN/chains/test-1/resume")
if [ "$RESUME_CODE" != "409" ]; then
  echo "FAIL: expected 409 resuming non-suspended chain, got $RESUME_CODE"
  exit 1
fi

echo "==> admin: gas-cache (populated after pour)"
GC=$(curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$ADMIN/chains/test-1/gas-cache")
echo "    $GC"
echo "$GC" | grep -q '"fee_denom"'
echo "$GC" | grep -q '"sample_count"'

echo "==> admin: gas-cache reset"
curl -sf -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$ADMIN/chains/test-1/gas-cache/reset" | grep -q '"ok":true'

echo "==> admin: unauthorized request returns 401"
AUTH_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  "$ADMIN/chains/test-1/status")
if [ "$AUTH_CODE" != "401" ]; then
  echo "FAIL: expected 401 without token, got $AUTH_CODE"
  exit 1
fi

echo "==> metrics"
METRICS=$(curl -sf "$BASE/metrics")
# Existing pour request counter: confirmed outcome must be present
echo "$METRICS" | grep 'pour_requests_total' | grep -q 'outcome="confirmed"'
# Refill loop emits distributor balance gauge (chain is live and refill ran before pour)
echo "$METRICS" | grep -q 'pour_distributor_balance{chain="test-1"'
# Refill counter: distributor starts at zero on a fresh chain so a top-up must have fired
echo "$METRICS" | grep -q 'pour_distributor_refill_total{chain="test-1"}'

echo "==> passed"