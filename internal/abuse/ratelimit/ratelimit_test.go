package ratelimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"

	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

func newTestLimiter(t *testing.T, requests int) *Limiter {
	t.Helper()
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s, requests, 5*time.Second)
}

func TestCheck_firstRequest(t *testing.T) {
	l := newTestLimiter(t, 3)
	if err := l.Check(t.Context(), "1.2.3.4", "osmosis-1"); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
}

func TestCheck_atLimit(t *testing.T) {
	l := newTestLimiter(t, 3)
	for range 3 {
		if err := l.Check(t.Context(), "1.2.3.4", "osmosis-1"); err != nil {
			t.Fatalf("request within limit should pass: %v", err)
		}
	}
}

func TestCheck_overLimit(t *testing.T) {
	l := newTestLimiter(t, 3)
	for range 3 {
		if err := l.Check(t.Context(), "1.2.3.4", "osmosis-1"); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	err := l.Check(t.Context(), "1.2.3.4", "osmosis-1")
	if err == nil {
		t.Fatal("expected ErrRateLimitExceeded, got nil")
	}
	var rl *ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("expected *ErrRateLimitExceeded, got %T: %v", err, err)
	}
	if rl.RetryAfter <= 0 {
		t.Errorf("RetryAfter should be positive, got %s", rl.RetryAfter)
	}
}

func TestCheck_windowExpiry(t *testing.T) {
	l := newTestLimiter(t, 2)

	// Insert an expired row directly (beyond the 5s window).
	expired := time.Now().Unix() - 10
	_, err := l.db.ExecContext(context.Background(), `
		INSERT INTO rate_limits (scope_type, scope_value, chain_id, window_start, request_count, coins_total)
		VALUES ('ip', '1.2.3.4', 'osmosis-1', ?, 2, '')
	`, expired)
	if err != nil {
		t.Fatalf("insert expired row: %v", err)
	}

	// Both requests in the current window should still pass (expired row ignored).
	for range 2 {
		if err := l.Check(t.Context(), "1.2.3.4", "osmosis-1"); err != nil {
			t.Fatalf("expired rows must not count: %v", err)
		}
	}
}

func TestCheck_differentChains(t *testing.T) {
	l := newTestLimiter(t, 1)
	if err := l.Check(t.Context(), "1.2.3.4", "osmosis-1"); err != nil {
		t.Fatalf("first chain: %v", err)
	}
	// Same IP, different chain — independent counter.
	if err := l.Check(t.Context(), "1.2.3.4", "cosmoshub-4"); err != nil {
		t.Fatalf("second chain should have independent counter: %v", err)
	}
}

func TestCheck_differentIPs(t *testing.T) {
	l := newTestLimiter(t, 1)
	if err := l.Check(t.Context(), "1.2.3.4", "osmosis-1"); err != nil {
		t.Fatalf("first IP: %v", err)
	}
	// Different IP, same chain — independent counter.
	if err := l.Check(t.Context(), "5.6.7.8", "osmosis-1"); err != nil {
		t.Fatalf("second IP should have independent counter: %v", err)
	}
}

func TestErrRateLimitExceeded_Error(t *testing.T) {
	e := &ErrRateLimitExceeded{RetryAfter: 5 * time.Second}
	want := "rate limit exceeded; retry after 5s"
	if got := e.Error(); got != want {
		t.Errorf("Error(): got %q, want %q", got, want)
	}
}

// --- CheckAddress tests ---

// newTestAddr generates a random secp256k1 key and returns a bech32 address with prefix.
func newTestAddr(t *testing.T, prefix string) string {
	t.Helper()
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	addr, err := tx.AddressFromPubKey(privKey.PubKey().SerializeCompressed(), prefix)
	if err != nil {
		t.Fatalf("AddressFromPubKey: %v", err)
	}
	return addr
}

func makeCoin(amount string) tx.Coin {
	return tx.Coin{Amount: amount, Denom: "uatom"}
}

func TestCheckAddress_pass(t *testing.T) {
	l := newTestLimiter(t, 10)
	addr := newTestAddr(t, "cosmos")
	drip := makeCoin("1000000")

	maxCoin := makeCoin("5000000")

	if err := l.CheckAddress(t.Context(), addr, "secp256k1", "cosmoshub-4", drip, maxCoin); err != nil {
		t.Fatalf("first drip should pass: %v", err)
	}
}

func TestCheckAddress_capEnforcement(t *testing.T) {
	l := newTestLimiter(t, 10)
	addr := newTestAddr(t, "cosmos")
	drip := makeCoin("2000000")
	maxCoin := makeCoin("3000000") // allows 1 drip (2M), blocks 2nd (4M > 3M)

	if err := l.CheckAddress(t.Context(), addr, "secp256k1", "cosmoshub-4", drip, maxCoin); err != nil {
		t.Fatalf("first drip should pass: %v", err)
	}

	err := l.CheckAddress(t.Context(), addr, "secp256k1", "cosmoshub-4", drip, maxCoin)
	var rl *ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("second drip should be rate-limited, got: %v", err)
	}
	if rl.RetryAfter <= 0 {
		t.Errorf("RetryAfter should be positive, got %s", rl.RetryAfter)
	}
}

func TestCheckAddress_windowExpiry(t *testing.T) {
	l := newTestLimiter(t, 10)
	addr := newTestAddr(t, "cosmos")
	drip := makeCoin("2000000")
	maxCoin := makeCoin("3000000")

	// Compute the normalized scope value for addr.
	rawBytes, err := tx.DecodeAddressBytes(addr)
	if err != nil {
		t.Fatalf("DecodeAddressBytes: %v", err)
	}
	scopeValue := "secp256k1:" + hex.EncodeToString(rawBytes)

	// Insert an expired row for this address (beyond 24h window).
	expired := time.Now().Unix() - int64(25*time.Hour/time.Second)
	if _, err = l.db.ExecContext(context.Background(), `
		INSERT INTO rate_limits (scope_type, scope_value, chain_id, window_start, request_count, coins_total)
		VALUES ('address', ?, 'cosmoshub-4', ?, 1, '2000000')
	`, scopeValue, expired); err != nil {
		t.Fatalf("insert expired row: %v", err)
	}

	// Expired row must not count — first drip should pass even though the cap is 3M.
	if err = l.CheckAddress(t.Context(), addr, "secp256k1", "cosmoshub-4", drip, maxCoin); err != nil {
		t.Fatalf("drip should pass when only expired rows exist: %v", err)
	}
}

func TestCheckAddress_crossChainNormalization(t *testing.T) {
	l := newTestLimiter(t, 10)
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	pub := privKey.PubKey().SerializeCompressed()

	cosmosAddr, err := tx.AddressFromPubKey(pub, "cosmos")
	if err != nil {
		t.Fatalf("AddressFromPubKey cosmos: %v", err)
	}
	osmoAddr, err := tx.AddressFromPubKey(pub, "osmo")
	if err != nil {
		t.Fatalf("AddressFromPubKey osmo: %v", err)
	}

	drip := makeCoin("2000000")
	maxCoin := makeCoin("3000000")

	// Drip using cosmos prefix.
	if err := l.CheckAddress(t.Context(), cosmosAddr, "secp256k1", "cosmoshub-4", drip, maxCoin); err != nil {
		t.Fatalf("cosmos drip should pass: %v", err)
	}
	// Same key via osmo prefix on same chain — should be blocked (same scope_value after normalization).
	err = l.CheckAddress(t.Context(), osmoAddr, "secp256k1", "cosmoshub-4", drip, maxCoin)
	var rl *ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("osmo drip (same key) should be rate-limited, got: %v", err)
	}
}

// --- CheckAPIKey tests ---

func randomKeyID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestCheckAPIKey_pass(t *testing.T) {
	l := newTestLimiter(t, 5)
	keyID := randomKeyID(t)
	if err := l.CheckAPIKey(t.Context(), keyID, "osmosis-1", 0); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
}

func TestCheckAPIKey_limit(t *testing.T) {
	l := newTestLimiter(t, 100)
	keyID := randomKeyID(t)

	// Custom per-key limit of 2.
	for range 2 {
		if err := l.CheckAPIKey(t.Context(), keyID, "osmosis-1", 2); err != nil {
			t.Fatalf("request within limit should pass: %v", err)
		}
	}

	err := l.CheckAPIKey(t.Context(), keyID, "osmosis-1", 2)
	var rl *ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("third request should be rate-limited, got: %v", err)
	}
	if rl.RetryAfter <= 0 {
		t.Errorf("RetryAfter should be positive, got %s", rl.RetryAfter)
	}
}

func TestCheckAPIKey_fallback(t *testing.T) {
	// requestsPerWindow=2, rateLimitPerHour=0 → falls back to 2.
	l := newTestLimiter(t, 2)
	keyID := randomKeyID(t)

	for range 2 {
		if err := l.CheckAPIKey(t.Context(), keyID, "osmosis-1", 0); err != nil {
			t.Fatalf("request within default limit should pass: %v", err)
		}
	}

	err := l.CheckAPIKey(t.Context(), keyID, "osmosis-1", 0)
	var rl *ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("third request should hit default limit, got: %v", err)
	}
}
