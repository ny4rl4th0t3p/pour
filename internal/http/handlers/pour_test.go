package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// ----- test doubles -----

type mockBroadcaster struct {
	result *tx.BroadcastResult
	err    error
}

func (m *mockBroadcaster) BuildAndBroadcast(_ context.Context, _ tx.SendRequest) (*tx.BroadcastResult, error) {
	return m.result, m.err
}

type mockRateLimiter struct{ err error }

func (m *mockRateLimiter) Check(_ context.Context, _, _ string) error { return m.err }

type mockDripStore struct{ id int64 }

func (m *mockDripStore) RecordDrip(_ context.Context, _ store.DripRecord) (int64, error) {
	return m.id, nil
}

// ----- helpers -----

var testChains = map[string]config.ChainConfig{
	"osmosis-1": {
		ChainID:      "osmosis-1",
		Enabled:      true,
		Bech32Prefix: "osmo",
		Drip:         config.DripConfig{Anonymous: "1000000uosmo"},
	},
}

func newTestHandler(t *testing.T, bc Broadcaster, rl RateLimiter, ds DripStore) *Handler {
	t.Helper()
	return New(Deps{
		Chains:       testChains,
		Broadcasters: map[string]Broadcaster{"osmosis-1": bc},
		Limiter:      rl,
		DripStore:    ds,
		Mnemonic:     "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
		Version:      "test",
	})
}

func pourRequest(body string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/pour", strings.NewReader(body))
	r.RemoteAddr = "1.2.3.4:55000"
	return r
}

// ----- tests -----

func TestPour_success(t *testing.T) {
	bc := &mockBroadcaster{result: &tx.BroadcastResult{TxHash: "TXHASH", Height: 100}}
	ds := &mockDripStore{id: 42}
	h := newTestHandler(t, bc, &mockRateLimiter{}, ds)

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.PourResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DripID != 42 {
		t.Errorf("DripID: got %d, want 42", resp.DripID)
	}
	if resp.TxHash != "TXHASH" {
		t.Errorf("TxHash: got %q, want TXHASH", resp.TxHash)
	}
	if resp.Amount != "1000000uosmo" {
		t.Errorf("Amount: got %q, want 1000000uosmo", resp.Amount)
	}
	if resp.Status != "confirmed" {
		t.Errorf("Status: got %q, want confirmed", resp.Status)
	}
}

func TestPour_chainNotFound(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockRateLimiter{}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"unknown-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func TestPour_invalidAddress(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockRateLimiter{}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"cosmos1wrongprefix"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_rateLimited(t *testing.T) {
	exc := &ratelimit.ErrRateLimitExceeded{RetryAfter: 30 * time.Second}
	h := newTestHandler(t, &mockBroadcaster{}, &mockRateLimiter{err: exc}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing")
	}
}

func TestPour_broadcastError(t *testing.T) {
	bc := &mockBroadcaster{err: tx.ErrChainUnreachable}
	h := newTestHandler(t, bc, &mockRateLimiter{}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestPour_badJSON(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockRateLimiter{}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_realLimiter_rateLimited(t *testing.T) {
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	limiter := ratelimit.New(s, 1, 5*time.Second)
	bc := &mockBroadcaster{result: &tx.BroadcastResult{TxHash: "TX1"}}

	h := New(Deps{
		Chains:       testChains,
		Broadcasters: map[string]Broadcaster{"osmosis-1": bc},
		Limiter:      limiter,
		DripStore:    s,
		Mnemonic:     "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
		Version:      "test",
	})

	// First request should succeed.
	w1 := httptest.NewRecorder()
	h.Pour(w1, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", w1.Code)
	}

	// Second request with same IP exceeds limit of 1.
	bc.result = &tx.BroadcastResult{TxHash: "TX2"}
	w2 := httptest.NewRecorder()
	h.Pour(w2, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", w2.Code)
	}
}
