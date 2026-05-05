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
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

var testLastChanged = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

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

// stubChainSource implements chain.ChainSource for handler tests.
type stubChainSource struct {
	snaps map[string]chain.ChainSnapshot
}

func (s *stubChainSource) GetActive(chainID string) (chain.ChainSnapshot, bool) {
	snap, ok := s.snaps[chainID]
	return snap, ok
}

func (s *stubChainSource) ListActive() []chain.ChainSnapshot {
	out := make([]chain.ChainSnapshot, 0, len(s.snaps))
	for _, snap := range s.snaps {
		out = append(out, snap)
	}
	return out
}

func (*stubChainSource) LastFetched() time.Time  { return time.Time{} }
func (*stubChainSource) PendingFrozenCount() int { return 0 }

// ----- helpers -----

var testSource = &stubChainSource{
	snaps: map[string]chain.ChainSnapshot{
		"osmosis-1": {
			Info: &chainregistry.ChainInfo{ChainID: "osmosis-1", Bech32Prefix: "osmo", Slip44: 118, LastChanged: testLastChanged},
			Drip: chainregistry.DripPolicy{Anonymous: "1000000uosmo"},
		},
	},
}

func newTestHandler(t *testing.T, bc Broadcaster, rl RateLimiter, ds DripStore) *Handler {
	t.Helper()
	return New(Deps{
		Source:       testSource,
		Broadcasters: map[string]Broadcaster{"osmosis-1": bc},
		Limiter:      rl,
		DripStore:    ds,
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
		Source:       testSource,
		Broadcasters: map[string]Broadcaster{"osmosis-1": bc},
		Limiter:      limiter,
		DripStore:    s,
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

// ----- chain detail tests -----

func newChainDetailRequest(chainID string) *http.Request {
	return httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/v1/chains/"+chainID, http.NoBody,
	)
}

func TestChainDetail_found(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockRateLimiter{}, &mockDripStore{})

	// chi router is needed for URL params; call via the router.
	router := h.testRouter()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newChainDetailRequest("osmosis-1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.ChainDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ChainID != "osmosis-1" {
		t.Errorf("ChainID: got %q, want osmosis-1", resp.ChainID)
	}
	if resp.Bech32Prefix != "osmo" {
		t.Errorf("Bech32Prefix: got %q, want osmo", resp.Bech32Prefix)
	}
	if resp.DripAmount != "1000000uosmo" {
		t.Errorf("DripAmount: got %q, want 1000000uosmo", resp.DripAmount)
	}
	if want := testLastChanged.UTC().Format(time.RFC3339); resp.LastChanged != want {
		t.Errorf("LastChanged: got %q, want %q", resp.LastChanged, want)
	}
}

func TestChainDetail_notFound(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockRateLimiter{}, &mockDripStore{})
	router := h.testRouter()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newChainDetailRequest("unknown-1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}
