package gascache

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/store"
)

func newTestCacheWithPrice(t *testing.T, price string) *Cache {
	t.Helper()
	c := newTestCache(t)
	if err := c.RecordSuccess(t.Context(), "osmosis-1", 150_000, "uosmo", price); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	return c
}

func currentPrice(t *testing.T, c *Cache) string {
	t.Helper()
	est, ok := c.Lookup(t.Context(), "osmosis-1")
	if !ok {
		t.Fatalf("no cache entry for osmosis-1")
	}
	return est.GasPriceAmount
}

func TestRunDecay_noFloor(t *testing.T) {
	c := newTestCacheWithPrice(t, "0.100")

	noFloor := func(string) (string, bool) { return "", false }
	if err := c.runDecay(t.Context(), noFloor); err != nil {
		t.Fatalf("runDecay: %v", err)
	}

	got := currentPrice(t, c)
	// 0.100 × 0.95 = 0.095
	if got != "0.095" {
		t.Errorf("price after decay: got %s, want 0.095", got)
	}
}

func TestRunDecay_withFloor(t *testing.T) {
	c := newTestCacheWithPrice(t, "0.100")

	withFloor := func(string) (string, bool) { return "0.05", true }
	if err := c.runDecay(t.Context(), withFloor); err != nil {
		t.Fatalf("runDecay: %v", err)
	}

	got := currentPrice(t, c)
	// 0.100 × 0.9 = 0.09 > 0.05 floor → 0.09
	if got != "0.09" {
		t.Errorf("price after decay: got %s, want 0.09", got)
	}
}

func TestRunDecay_floorEnforced(t *testing.T) {
	c := newTestCacheWithPrice(t, "0.050")

	// floor = 0.05, current = 0.05 × 0.9 = 0.045 < floor → stays at 0.05
	withFloor := func(string) (string, bool) { return "0.05", true }
	if err := c.runDecay(t.Context(), withFloor); err != nil {
		t.Fatalf("runDecay: %v", err)
	}

	got := currentPrice(t, c)
	if got != "0.05" {
		t.Errorf("price should stay at floor: got %s, want 0.05", got)
	}
}

func TestRunDecay_alreadyAtFloor(t *testing.T) {
	c := newTestCacheWithPrice(t, "0.025")

	withFloor := func(string) (string, bool) { return "0.025", true }
	if err := c.runDecay(t.Context(), withFloor); err != nil {
		t.Fatalf("runDecay: %v", err)
	}

	got := currentPrice(t, c)
	// current == floor → target == floor == current → no update
	if got != "0.025" {
		t.Errorf("price should not change: got %s, want 0.025", got)
	}
}

func TestStart_cancelsCleanly(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	c := New(s)
	c.interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	c.Start(ctx, func(string) (string, bool) { return "", false })

	cancel()
	// Give the goroutine time to exit; no deadlock or panic expected.
	time.Sleep(50 * time.Millisecond)
}
