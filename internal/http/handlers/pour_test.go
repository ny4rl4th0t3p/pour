package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/abuse"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/batch"
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
	calls  int
}

func (m *mockBroadcaster) BuildAndBroadcast(_ context.Context, _ tx.SendRequest) (*tx.BroadcastResult, error) {
	m.calls++
	return m.result, m.err
}

// mockAdmitter stubs the Gate for pour handler tests.
type mockAdmitter struct {
	decision *abuse.Decision
	err      error
}

func (m *mockAdmitter) Admit(_ context.Context, _ *http.Request, _ *pourapi.PourRequest, _ abuse.ChainContext) (*abuse.Decision, error) {
	return m.decision, m.err
}

func okAdmitter() *mockAdmitter {
	return &mockAdmitter{decision: &abuse.Decision{
		Mechanism: abuse.MechanismAnonymous,
		DripCoin:  tx.Coin{Amount: "1000000", Denom: "uosmo"},
	}}
}

type mockDripStore struct{ id int64 }

func (m *mockDripStore) RecordDrip(_ context.Context, _ store.DripRecord) (int64, error) {
	return m.id, nil
}

func (*mockDripStore) UpdateDrip(_ context.Context, _ int64, _, _ string, _ int64) error {
	return nil
}

// stubChainSource implements chain.ChainSource for handler tests.
type stubChainSource struct {
	snaps             map[string]chain.ChainSnapshot
	channels          map[string][]chainregistry.IBCChannel
	allChannels       []chainregistry.IBCChannel
	ibcTransferResult tx.TransferResult
	ibcTransferErr    error
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

func (s *stubChainSource) ChannelsFor(chainName string) []chainregistry.IBCChannel {
	return s.channels[chainName]
}

func (s *stubChainSource) AllIBCChannels() []chainregistry.IBCChannel {
	return s.allChannels
}

func (s *stubChainSource) IBCTransfer(_ context.Context, _ string, _ tx.TransferRequest) (tx.TransferResult, error) {
	return s.ibcTransferResult, s.ibcTransferErr
}

// ----- helpers -----

var testSource = &stubChainSource{
	snaps: map[string]chain.ChainSnapshot{
		"osmosis-1": {
			Info: &chainregistry.ChainInfo{
				ChainID:      "osmosis-1",
				Bech32Prefix: "osmo",
				Slip44:       118,
				KeyAlgo:      chainregistry.KeyAlgoSecp256k1,
				LastChanged:  testLastChanged,
			},
			Drip: chainregistry.DripPolicy{
				Anonymous:           "1000000uosmo",
				MaxPerAddressPerDay: "50000000uosmo",
			},
		},
	},
}

func newTestHandler(t *testing.T, bc Broadcaster, gate Admitter, ds DripStore) *Handler {
	t.Helper()
	return New(Deps{
		Source:       testSource,
		Broadcasters: map[string]Broadcaster{"osmosis-1": bc},
		Gate:         gate,
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
	h := newTestHandler(t, bc, okAdmitter(), ds)

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
	if resp.Mechanism != string(abuse.MechanismAnonymous) {
		t.Errorf("Mechanism: got %q, want %q", resp.Mechanism, abuse.MechanismAnonymous)
	}
}

func TestPour_chainNotFound(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, okAdmitter(), &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"unknown-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func TestPour_invalidAddress(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, okAdmitter(), &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"cosmos1wrongprefix"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_rateLimited(t *testing.T) {
	rlErr := &ratelimit.ErrRateLimitExceeded{RetryAfter: 30 * time.Second}
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: rlErr}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing")
	}
}

func TestPour_unauthenticated(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrUnauthenticated}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", w.Code)
	}
}

func TestPour_forbidden(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrForbidden}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403", w.Code)
	}
}

func TestPour_powRequired(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrPoWRequired}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_badPoW(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrBadPoW}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_badNonce(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrBadNonce}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_badSignature(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrBadSignature}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestPour_predicateFailed(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, &mockAdmitter{err: abuse.ErrPredicateFailed}, &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403", w.Code)
	}
}

func TestPour_broadcastError(t *testing.T) {
	bc := &mockBroadcaster{err: tx.ErrChainUnreachable}
	h := newTestHandler(t, bc, okAdmitter(), &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestPour_badJSON(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, okAdmitter(), &mockDripStore{})
	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

// capturingDripStore wraps a real store and signals when UpdateDrip is called.
type capturingDripStore struct {
	*store.Store
	updated chan updateCall
}

type updateCall struct {
	status string
	txHash string
}

func (c *capturingDripStore) UpdateDrip(ctx context.Context, id int64, status, txHash string, completedAt int64) error {
	err := c.Store.UpdateDrip(ctx, id, status, txHash, completedAt)
	if err == nil {
		c.updated <- updateCall{status: status, txHash: txHash}
	}
	return err
}

func TestPour_async_dripRecordTransition(t *testing.T) {
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ds := &capturingDripStore{Store: s, updated: make(chan updateCall, 1)}
	pourer := &mockPourer{result: batch.Result{TxHash: "INTEGRATION_TX"}}
	h := New(Deps{
		Source:    testSource,
		Pourer:    pourer,
		Gate:      okAdmitter(),
		DripStore: ds,
		Version:   "test",
	})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))

	if w.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", w.Code)
	}

	select {
	case got := <-ds.updated:
		if got.status != pourapi.StatusConfirmed {
			t.Errorf("drip status: got %q, want %q", got.status, pourapi.StatusConfirmed)
		}
		if got.txHash != "INTEGRATION_TX" {
			t.Errorf("drip txHash: got %q, want INTEGRATION_TX", got.txHash)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: UpdateDrip not called within 5s")
	}
}

// mockPourer simulates the async batch pool: routing succeeds, then delivers result.
type mockPourer struct {
	err    error // if non-nil, Pour returns this immediately
	result batch.Result
}

func (m *mockPourer) Pour(_ string, req batch.Request) error {
	if m.err != nil {
		return m.err
	}
	go func() { req.Result <- m.result }()
	return nil
}

func newAsyncTestHandler(t *testing.T, pourer ChainPourer, ds DripStore) *Handler {
	t.Helper()
	return New(Deps{
		Source:    testSource,
		Pourer:    pourer,
		Gate:      okAdmitter(),
		DripStore: ds,
		Version:   "test",
	})
}

// ----- async pour tests -----

func TestPour_async_queued(t *testing.T) {
	ds := &mockDripStore{id: 7}
	pourer := &mockPourer{result: batch.Result{TxHash: "ASYNC_TX"}}
	h := newAsyncTestHandler(t, pourer, ds)

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))

	if w.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", w.Code)
	}
	var resp pourapi.PourResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != pourapi.StatusQueued {
		t.Errorf("status: got %q, want %q", resp.Status, pourapi.StatusQueued)
	}
	if resp.DripID != 7 {
		t.Errorf("drip_id: got %d, want 7", resp.DripID)
	}
	if resp.TxHash != "" {
		t.Errorf("tx_hash: want empty for queued response, got %q", resp.TxHash)
	}
	if resp.Amount != "1000000uosmo" {
		t.Errorf("amount: got %q, want 1000000uosmo", resp.Amount)
	}
}

func TestPour_async_suspended(t *testing.T) {
	pourer := &mockPourer{err: chain.ErrChainSuspended}
	h := newAsyncTestHandler(t, pourer, &mockDripStore{})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestPour_async_queueFull(t *testing.T) {
	pourer := &mockPourer{err: batch.ErrAllFull}
	h := newAsyncTestHandler(t, pourer, &mockDripStore{})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestPour_async_syncFallback(t *testing.T) {
	// When Pourer returns ErrSyncMode, handler falls through to broadcaster.
	pourer := &mockPourer{err: chain.ErrSyncMode}
	bc := &mockBroadcaster{result: &tx.BroadcastResult{TxHash: "SYNC_TX"}}
	ds := &mockDripStore{id: 3}
	h := New(Deps{
		Source:       testSource,
		Pourer:       pourer,
		Broadcasters: map[string]Broadcaster{"osmosis-1": bc},
		Gate:         okAdmitter(),
		DripStore:    ds,
		Version:      "test",
	})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.PourResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != pourapi.StatusConfirmed {
		t.Errorf("status: got %q, want %q", resp.Status, pourapi.StatusConfirmed)
	}
	if resp.TxHash != "SYNC_TX" {
		t.Errorf("tx_hash: got %q, want SYNC_TX", resp.TxHash)
	}
}

// ----- chain detail tests -----

func newChainDetailRequest(chainID string) *http.Request {
	return httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/v1/chains/"+chainID, http.NoBody,
	)
}

func TestChainDetail_found(t *testing.T) {
	h := newTestHandler(t, &mockBroadcaster{}, okAdmitter(), &mockDripStore{})

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
	h := newTestHandler(t, &mockBroadcaster{}, okAdmitter(), &mockDripStore{})
	router := h.testRouter()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newChainDetailRequest("unknown-1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func ibcTestSource(ibcTransferResult tx.TransferResult, ibcTransferErr error, withChannel bool) *stubChainSource {
	channels := map[string][]chainregistry.IBCChannel{}
	if withChannel {
		channels["simapp-a"] = []chainregistry.IBCChannel{{
			ChainNameA: "simapp-a",
			ChainNameB: "simapp-b",
			ChannelA:   "channel-0",
			ChannelB:   "channel-0",
			PortA:      "transfer",
			PortB:      "transfer",
			Status:     "live",
			Preferred:  true,
		}}
	}
	return &stubChainSource{
		snaps: map[string]chain.ChainSnapshot{
			"simapp-b-1": {
				Info: &chainregistry.ChainInfo{
					ChainID:      "simapp-b-1",
					ChainName:    "simapp-b",
					Bech32Prefix: "cosmos",
					Slip44:       118,
					KeyAlgo:      chainregistry.KeyAlgoSecp256k1,
				},
				Drip: chainregistry.DripPolicy{
					Anonymous:           "1000000uosmo",
					MaxPerAddressPerDay: "50000000uosmo",
				},
				IBCSourceChainID: "simapp-a-1",
			},
			"simapp-a-1": {
				Info: &chainregistry.ChainInfo{
					ChainID:      "simapp-a-1",
					ChainName:    "simapp-a",
					Bech32Prefix: "cosmos",
					Slip44:       118,
					KeyAlgo:      chainregistry.KeyAlgoSecp256k1,
				},
				Drip: chainregistry.DripPolicy{
					Anonymous:           "1000000ustake",
					MaxPerAddressPerDay: "50000000ustake",
				},
			},
		},
		channels:          channels,
		ibcTransferResult: ibcTransferResult,
		ibcTransferErr:    ibcTransferErr,
	}
}

func TestPour_IBCRoute(t *testing.T) {
	src := ibcTestSource(tx.TransferResult{TxHash: "IBC_TX1"}, nil, true)
	bc := &mockBroadcaster{}
	ds := &mockDripStore{id: 7}
	h := New(Deps{
		Source:       src,
		Broadcasters: map[string]Broadcaster{"simapp-a-1": bc},
		Gate:         okAdmitter(),
		DripStore:    ds,
		Version:      "test",
	})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"simapp-b-1","address":"cosmos1abc123defg"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.PourResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != pourapi.StatusConfirmed {
		t.Errorf("Status: got %q, want confirmed", resp.Status)
	}
	if resp.TxHash != "IBC_TX1" {
		t.Errorf("TxHash: got %q, want IBC_TX1", resp.TxHash)
	}
	if resp.Mechanism != "ibc" {
		t.Errorf("Mechanism: got %q, want ibc", resp.Mechanism)
	}
	if bc.calls != 0 {
		t.Errorf("BuildAndBroadcast calls: got %d, want 0", bc.calls)
	}
}

func TestPour_IBCRoute_NoChannel(t *testing.T) {
	src := ibcTestSource(tx.TransferResult{}, nil, false)
	h := New(Deps{
		Source:    src,
		Gate:      okAdmitter(),
		DripStore: &mockDripStore{},
		Version:   "test",
	})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"simapp-b-1","address":"cosmos1abc123defg"}`))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	var errResp pourapi.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(errResp.Error, "no IBC channel") {
		t.Errorf("error %q: want it to mention no IBC channel", errResp.Error)
	}
}

func TestPour_IBCRoute_SourceNotActive(t *testing.T) {
	// dest chain exists but its source chain is not registered — simulates a
	// misconfigured or not-yet-started source chain.
	src := &stubChainSource{
		snaps: map[string]chain.ChainSnapshot{
			"simapp-b-1": {
				Info: &chainregistry.ChainInfo{
					ChainID:      "simapp-b-1",
					ChainName:    "simapp-b",
					Bech32Prefix: "cosmos",
					Slip44:       118,
					KeyAlgo:      chainregistry.KeyAlgoSecp256k1,
				},
				Drip: chainregistry.DripPolicy{
					Anonymous:           "1000000uosmo",
					MaxPerAddressPerDay: "50000000uosmo",
				},
				IBCSourceChainID: "simapp-a-1",
			},
			// simapp-a-1 intentionally absent
		},
	}
	h := New(Deps{
		Source:    src,
		Gate:      okAdmitter(),
		DripStore: &mockDripStore{},
		Version:   "test",
	})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"simapp-b-1","address":"cosmos1abc123defg"}`))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	var errResp pourapi.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(errResp.Error, "source chain not active") {
		t.Errorf("error %q: want it to mention source chain not active", errResp.Error)
	}
}

func TestPour_IBCRoute_TransferError(t *testing.T) {
	src := ibcTestSource(tx.TransferResult{}, errors.New("rpc unavailable"), true)
	h := New(Deps{
		Source:    src,
		Gate:      okAdmitter(),
		DripStore: &mockDripStore{},
		Version:   "test",
	})

	w := httptest.NewRecorder()
	h.Pour(w, pourRequest(`{"chain_id":"simapp-b-1","address":"cosmos1abc123defg"}`))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want 502", w.Code)
	}
	var errResp pourapi.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(errResp.Error, "IBC transfer failed") {
		t.Errorf("error %q: want it to mention IBC transfer failed", errResp.Error)
	}
}
