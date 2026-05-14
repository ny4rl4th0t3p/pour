package chain

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func newTestGasCache(t *testing.T) *gascache.Cache {
	t.Helper()
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return gascache.New(s)
}

func enabledPtr(v bool) *bool    { return &v }
func strPtr(s string) *string    { return &s }
func uint32Ptr(v uint32) *uint32 { return &v }

// ibcDestChainCfg builds a minimal standalone IBC-only destination ChainConfig for tests.
// No drip.anonymous — all drips arrive via MsgTransfer from the source chain, so no tx
// client is created on the destination side (client == nil).
func ibcDestChainCfg(chainID, sourceChainID string) config.ChainConfig {
	return config.ChainConfig{
		ChainID:      chainID,
		Standalone:   true,
		Enabled:      enabledPtr(true),
		Bech32Prefix: strPtr("dest"),
		Slip44:       uint32Ptr(118),
		FeeTokens:    []config.FeeTokenConfig{{Denom: "udest"}},
		IBC: config.IBCConfig{
			Timeout: "10m",
			Drips: []config.IBCDripConfig{{
				SourceChainID:       sourceChainID,
				Anonymous:           "1000000udest",
				MaxPerAddressPerDay: "50000000udest",
			}},
		},
	}
}

// standaloneChainCfg builds a minimal standalone ChainConfig for tests.
// It uses localhost:9999 as the gRPC endpoint — gRPC dials lazily so no connection is attempted.
func standaloneChainCfg(chainID, bech32 string, enabled bool) config.ChainConfig {
	return config.ChainConfig{
		ChainID:      chainID,
		Standalone:   true,
		Enabled:      enabledPtr(enabled),
		Bech32Prefix: strPtr(bech32),
		Slip44:       uint32Ptr(118),
		Endpoints:    &config.EndpointsConfig{GRPC: []string{"localhost:9999"}},
		FeeTokens:    []config.FeeTokenConfig{{Denom: "utest"}},
		Drip: config.DripConfig{
			Anonymous:           "1000000utest",
			MaxPerAddressPerDay: "50000000utest",
		},
	}
}

func TestManager_standaloneEnabled(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("mynet-1", "mynet", true),
		},
	}

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	active := m.ListActive()
	if len(active) != 1 {
		t.Fatalf("ListActive: got %d, want 1", len(active))
	}
	if active[0].Info.ChainID != "mynet-1" {
		t.Errorf("ChainID: got %q, want mynet-1", active[0].Info.ChainID)
	}
	if active[0].Info.Bech32Prefix != "mynet" {
		t.Errorf("Bech32Prefix: got %q, want mynet", active[0].Info.Bech32Prefix)
	}
	if active[0].Drip.Anonymous != "1000000utest" {
		t.Errorf("Drip.Anonymous: got %q", active[0].Drip.Anonymous)
	}
}

func TestManager_standaloneDisabled(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("mynet-1", "mynet", false),
		},
	}

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	if active := m.ListActive(); len(active) != 0 {
		t.Errorf("ListActive: got %d, want 0 (disabled chain)", len(active))
	}
}

func TestManager_getActive_enabled(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("mynet-1", "mynet", true),
		},
	}

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	snap, ok := m.GetActive("mynet-1")
	if !ok {
		t.Fatal("GetActive: expected found, got false")
	}
	if snap.Info.ChainID != "mynet-1" {
		t.Errorf("ChainID: got %q", snap.Info.ChainID)
	}
}

func TestManager_getActive_unknown(t *testing.T) {
	m, err := New(context.Background(), Options{
		Config:     &config.ChainsConfig{},
		GasCache:   newTestGasCache(t),
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	if _, ok := m.GetActive("unknown-1"); ok {
		t.Error("GetActive: expected false for unknown chain, got true")
	}
}

func TestManager_getActive_disabled(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("mynet-1", "mynet", false),
		},
	}

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	if _, ok := m.GetActive("mynet-1"); ok {
		t.Error("GetActive: expected false for disabled chain, got true")
	}
}

func TestManager_pendingFrozenCount_zero(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("mynet-1", "mynet", true)},
	}
	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	if n := m.PendingFrozenCount(); n != 0 {
		t.Errorf("PendingFrozenCount: got %d, want 0", n)
	}
}

func TestManager_reload_updatesDripPolicy(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("mynet-1", "mynet", true)},
	}
	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	snap, ok := m.GetActive("mynet-1")
	if !ok {
		t.Fatal("chain not active before reload")
	}
	if snap.Drip.Anonymous != "1000000utest" {
		t.Fatalf("initial drip: got %q", snap.Drip.Anonymous)
	}

	// Reload with updated drip amount.
	updated := standaloneChainCfg("mynet-1", "mynet", true)
	updated.Drip.Anonymous = "2000000utest"
	newCfg := &config.ChainsConfig{Chains: []config.ChainConfig{updated}}
	if err := m.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	snap, ok = m.GetActive("mynet-1")
	if !ok {
		t.Fatal("chain not active after reload")
	}
	if snap.Drip.Anonymous != "2000000utest" {
		t.Errorf("drip after reload: got %q, want 2000000utest", snap.Drip.Anonymous)
	}
}

func TestManager_multipleChains(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("alpha-1", "alpha", true),
			standaloneChainCfg("beta-1", "beta", false),
			standaloneChainCfg("gamma-1", "gamma", true),
		},
	}

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	active := m.ListActive()
	if len(active) != 2 {
		t.Fatalf("ListActive: got %d, want 2", len(active))
	}
	// ListActive returns sorted by chain ID.
	if active[0].Info.ChainID != "alpha-1" || active[1].Info.ChainID != "gamma-1" {
		t.Errorf("unexpected chain IDs: %q, %q", active[0].Info.ChainID, active[1].Info.ChainID)
	}
}

func TestManager_lastFetched_standaloneOnlyIsZero(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("mynet-1", "mynet", true)},
	}
	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)
	if !m.LastFetched().IsZero() {
		t.Error("LastFetched: expected zero time for standalone-only manager")
	}
}

func TestManager_store_nonNil(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("mynet-1", "mynet", true)},
	}
	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)
	if m.Store() == nil {
		t.Error("Store: expected non-nil")
	}
}

func TestManager_clients_containsActiveChains(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("mynet-1", "mynet", true)},
	}
	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	clients := m.Clients()
	if len(clients) != 1 {
		t.Fatalf("Clients: got %d entries, want 1", len(clients))
	}
	if clients["mynet-1"] == nil {
		t.Error("Clients: mynet-1 entry is nil")
	}
}

// recordingHandler captures slog records for test assertions.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Clone())
	h.mu.Unlock()
	return nil
}
func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }
func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

func TestManager_logChangeSet(t *testing.T) {
	lh := &recordingHandler{}
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("mynet-1", "mynet", true)},
	}
	m, err := New(context.Background(), Options{
		Config:     cfg,
		GasCache:   newTestGasCache(t),
		Logger:     slog.New(lh),
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	lh.mu.Lock()
	lh.records = nil // discard any construction-time log records
	lh.mu.Unlock()

	cs := &chainregistry.ChangeSet{
		Warned: []chainregistry.FieldChange{
			{ChainID: "mynet-1", Field: "avg_gas_price", OldValue: "0.01", NewValue: "0.02"},
		},
		Frozen: []chainregistry.FieldChange{
			{ChainID: "mynet-1", Field: "bech32_prefix", OldValue: "mynet", NewValue: "newnet"},
		},
	}
	m.logChangeSet(cs)

	if n := lh.count(); n != 2 {
		t.Errorf("logChangeSet: got %d log records, want 2 (one warned, one frozen)", n)
	}
}

// minimalChainJSON is a valid rawChainInfo JSON for a registry chain named "mychain".
const minimalChainJSON = `{
	"chain_id":"mychain-1","chain_name":"mychain","bech32_prefix":"mychain",
	"slip44":118,"network_type":"testnet","key_algos":["secp256k1"],
	"fees":{"fee_tokens":[{"denom":"umychain","average_gas_price":"0.025"}]},
	"apis":{"grpc":[{"address":"localhost:9999"}]}
}`

func TestManager_refresh(t *testing.T) {
	fetches := make(chan struct{}, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, minimalChainJSON)
	}))
	defer srv.Close()

	enabled := true
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID: "mychain-1",
			Enabled: &enabled,
			Drip:    config.DripConfig{Anonymous: "1000000umychain", MaxPerAddressPerDay: "10000000umychain"},
		}},
	}
	m, err := New(context.Background(), Options{
		Config:          cfg,
		GasCache:        newTestGasCache(t),
		RegistryBaseURL: srv.URL,
		MnemonicFn:      func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	// Drain the initial fetch from New().
	select {
	case <-fetches:
	case <-time.After(5 * time.Second):
		t.Fatal("initial fetch timed out")
	}

	cs, err := m.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if cs == nil {
		t.Fatal("Refresh: expected non-nil ChangeSet")
	}
	if m.LastFetched().IsZero() {
		t.Error("LastFetched: expected non-zero after Refresh")
	}
	if _, ok := m.GetActive("mychain-1"); !ok {
		t.Error("chain should be active after Refresh")
	}
}

func TestManager_refreshLoop(t *testing.T) {
	fetches := make(chan struct{}, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, minimalChainJSON)
	}))
	defer srv.Close()

	enabled := true
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID: "mychain-1",
			Enabled: &enabled,
			Drip:    config.DripConfig{Anonymous: "1000000umychain", MaxPerAddressPerDay: "10000000umychain"},
		}},
	}
	m, err := New(context.Background(), Options{
		Config:          cfg,
		GasCache:        newTestGasCache(t),
		RegistryBaseURL: srv.URL,
		RefreshInterval: 50 * time.Millisecond,
		MnemonicFn:      func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	// Drain the initial fetch that happened inside New().
	select {
	case <-fetches:
	case <-time.After(5 * time.Second):
		t.Fatal("initial fetch timed out")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.StartRefreshLoop(ctx)

	// Wait for the loop to fire at least once.
	select {
	case <-fetches:
	case <-time.After(2 * time.Second):
		t.Fatal("refreshLoop did not fetch within 2s")
	}
	cancel()

	if _, ok := m.GetActive("mychain-1"); !ok {
		t.Error("chain should remain active after refresh loop tick")
	}
}

func TestManager_IBCTransfer_sourceNotFound(t *testing.T) {
	m, err := New(context.Background(), Options{
		Config:     &config.ChainsConfig{},
		GasCache:   newTestGasCache(t),
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	_, err = m.IBCTransfer(context.Background(), "nonexistent-1", tx.TransferRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("error %q: want it to mention 'not active'", err.Error())
	}
}

func TestManager_IBCTransfer_nilClient(t *testing.T) {
	// dest-1 is an IBC-destination chain (client == nil); src-1 is the source.
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("src-1", "src", true),
			ibcDestChainCfg("dest-1", "src-1"),
		},
	}
	m, err := New(context.Background(), Options{
		Config:     cfg,
		GasCache:   newTestGasCache(t),
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	// Passing dest-1 as the source chain ID must fail: it has no tx client.
	_, err = m.IBCTransfer(context.Background(), "dest-1", tx.TransferRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no tx client") {
		t.Errorf("error %q: want it to mention 'no tx client'", err.Error())
	}
}

func TestManager_IBCTransfer_routesToSource(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			standaloneChainCfg("src-1", "src", true),
		},
	}
	m, err := New(context.Background(), Options{
		Config:     cfg,
		GasCache:   newTestGasCache(t),
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	// IBCTransfer routes to src-1's tx.Client. The client dials localhost:9999 which
	// has no server, so we get a connection-level error — but NOT a manager guard error.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = m.IBCTransfer(ctx, "src-1", tx.TransferRequest{
		KeyIndex:         0,
		SourcePort:       "transfer",
		SourceChannel:    "channel-0",
		Token:            tx.Coin{Denom: "utest", Amount: "1000000"},
		ReceiverAddress:  "cosmos1test",
		TimeoutTimestamp: 1,
	})
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	if strings.Contains(err.Error(), "not active") || strings.Contains(err.Error(), "no tx client") {
		t.Errorf("error %q: routing did not reach the tx client (manager guard hit instead)", err.Error())
	}
}

// sourceOnlyChainCfg builds a standalone chain with endpoints but no native drip
// and no ibc.drips — an IBC source-only chain that can broadcast MsgTransfer but
// does not itself serve pour requests.
func sourceOnlyChainCfg(chainID string) config.ChainConfig {
	return config.ChainConfig{
		ChainID:      chainID,
		Standalone:   true,
		Enabled:      enabledPtr(true),
		Bech32Prefix: strPtr("cosmos"),
		Slip44:       uint32Ptr(118),
		Endpoints:    &config.EndpointsConfig{GRPC: []string{"localhost:9999"}},
		FeeTokens:    []config.FeeTokenConfig{{Denom: "ustake"}},
	}
}

func TestManager_sourceOnlyChain_hasTxClient(t *testing.T) {
	// Source-only chain has endpoints but no native drip. It must get a tx client
	// so it can broadcast MsgTransfer for another chain's IBC drips.
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			sourceOnlyChainCfg("hub-1"),
		},
	}
	m, err := New(context.Background(), Options{
		Config:     cfg,
		GasCache:   newTestGasCache(t),
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	c, ok := m.GetChain("hub-1")
	if !ok {
		t.Fatal("GetChain hub-1: not active")
	}
	if c.Client() == nil {
		t.Error("source-only chain: Client() is nil; expected a tx client for MsgTransfer")
	}
	if c.pool != nil {
		t.Error("source-only chain: pool is non-nil; expected no batch pool")
	}
	// IBCTransfer must reach the tx client (connection error, not a guard error).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = m.IBCTransfer(ctx, "hub-1", tx.TransferRequest{
		KeyIndex:         0,
		SourcePort:       "transfer",
		SourceChannel:    "channel-0",
		Token:            tx.Coin{Denom: "ustake", Amount: "1000000"},
		ReceiverAddress:  "cosmos1test",
		TimeoutTimestamp: 1,
	})
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	if strings.Contains(err.Error(), "no tx client") {
		t.Errorf("error %q: IBCTransfer hit the 'no tx client' guard instead of the tx client", err.Error())
	}
}
