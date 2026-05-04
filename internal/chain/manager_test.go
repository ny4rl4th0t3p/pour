package chain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	"github.com/ny4rl4th0t3p/pour/internal/store"
)

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

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t)})
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

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t)})
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

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t)})
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
		Config:   &config.ChainsConfig{},
		GasCache: newTestGasCache(t),
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

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(m.Close)

	if _, ok := m.GetActive("mynet-1"); ok {
		t.Error("GetActive: expected false for disabled chain, got true")
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

	m, err := New(context.Background(), Options{Config: cfg, GasCache: newTestGasCache(t)})
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
