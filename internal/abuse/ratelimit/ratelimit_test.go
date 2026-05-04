package ratelimit

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/store"
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
