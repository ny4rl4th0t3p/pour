package gascache

import (
	"path/filepath"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/store"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s)
}

func TestLookup_miss(t *testing.T) {
	c := newTestCache(t)
	_, ok := c.Lookup(t.Context(), "osmosis-1")
	if ok {
		t.Error("expected miss, got hit")
	}
}

func TestRecordSuccess_firstEntry(t *testing.T) {
	c := newTestCache(t)

	if err := c.RecordSuccess(t.Context(), "osmosis-1", 150_000, "uosmo", "0.025"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}

	est, ok := c.Lookup(t.Context(), "osmosis-1")
	if !ok {
		t.Fatal("expected hit after RecordSuccess")
	}
	if est.BaseGas != 150_000 {
		t.Errorf("BaseGas: got %d, want 150000", est.BaseGas)
	}
	if est.FeeDenom != "uosmo" {
		t.Errorf("FeeDenom: got %s, want uosmo", est.FeeDenom)
	}
	if est.SampleCount != 1 {
		t.Errorf("SampleCount: got %d, want 1", est.SampleCount)
	}
	if est.IsTrusted() {
		t.Error("1 sample should not be trusted")
	}
}

func TestRecordSuccess_movingAverage(t *testing.T) {
	c := newTestCache(t)

	for range 5 {
		if err := c.RecordSuccess(t.Context(), "osmosis-1", 180_000, "uosmo", "0.025"); err != nil {
			t.Fatalf("RecordSuccess: %v", err)
		}
	}

	est, ok := c.Lookup(t.Context(), "osmosis-1")
	if !ok {
		t.Fatal("expected hit")
	}
	if est.SampleCount != 5 {
		t.Errorf("SampleCount: got %d, want 5", est.SampleCount)
	}
	if !est.IsTrusted() {
		t.Error("5 samples should be trusted")
	}
	// All observations identical → average must equal the observed value.
	if est.BaseGas != 180_000 {
		t.Errorf("BaseGas: got %d, want 180000", est.BaseGas)
	}
}

func TestRecordFailure(t *testing.T) {
	c := newTestCache(t)

	if err := c.RecordSuccess(t.Context(), "osmosis-1", 150_000, "uosmo", "0.025"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}

	if err := c.RecordFailure(t.Context(), "osmosis-1", "out_of_gas"); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	// Gas values must not have changed.
	est, ok := c.Lookup(t.Context(), "osmosis-1")
	if !ok {
		t.Fatal("expected hit")
	}
	if est.BaseGas != 150_000 {
		t.Errorf("BaseGas changed after failure: got %d", est.BaseGas)
	}

	// Confirm failure reason was persisted.
	var reason string
	err := c.db.QueryRowContext(t.Context(),
		`SELECT last_failure_reason FROM chain_gas_cache WHERE chain_id = ?`, "osmosis-1",
	).Scan(&reason)
	if err != nil {
		t.Fatalf("query failure reason: %v", err)
	}
	if reason != "out_of_gas" {
		t.Errorf("failure reason: got %s, want out_of_gas", reason)
	}
}
