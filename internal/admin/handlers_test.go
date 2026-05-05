package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// ----- auth middleware tests -----

func newTokenStore() *TokenStore { return &TokenStore{token: "secret"} }

func newMWRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
}

func TestMiddleware_missingToken(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: got %d, want 401", w.Code)
	}
}

func TestMiddleware_wrongToken(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", w.Code)
	}
}

func TestMiddleware_forbiddenIP(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "10.0.0.1:1234" // not loopback
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("forbidden IP: got %d, want 403", w.Code)
	}
}

func TestMiddleware_allowed(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("allowed request: got %d, want 200", w.Code)
	}
}

func TestMiddleware_ipv6Loopback(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "[::1]:1234"
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("IPv6 loopback: got %d, want 200", w.Code)
	}
}

// ----- handler tests -----

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	regStore, err := chainregistry.New(chainregistry.Options{})
	if err != nil {
		t.Fatalf("chainregistry.New: %v", err)
	}
	return New(Deps{RegStore: regStore})
}

func TestHandler_snapshot_empty(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/registry/snapshot", http.NoBody)
	w := httptest.NewRecorder()
	h.Snapshot(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var chains []*chainregistry.ChainInfo
	if err := json.NewDecoder(w.Body).Decode(&chains); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(chains) != 0 {
		t.Errorf("expected empty snapshot, got %d chains", len(chains))
	}
}

func TestHandler_pending_empty(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/registry/pending", http.NoBody)
	w := httptest.NewRecorder()
	h.Pending(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var pending []*chainregistry.PendingChange
	if err := json.NewDecoder(w.Body).Decode(&pending); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected empty pending, got %d", len(pending))
	}
}

func TestHandler_accept_badBody(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader("not json"))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body: got %d, want 400", w.Code)
	}
}

func TestHandler_accept_missingChainID(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader(`{"field":"Bech32Prefix"}`))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing chain_id: got %d, want 400", w.Code)
	}
}

func TestHandler_accept_notFound(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader(`{"chain_id":"osmosis-1","field":"Bech32Prefix"}`))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("not found: got %d, want 404", w.Code)
	}
}

func TestHandler_accept_allFields_notFound(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader(`{"chain_id":"osmosis-1"}`))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("all fields not found: got %d, want 404", w.Code)
	}
}

// ----- refresh / reload handler tests -----

// standaloneOnlyManager builds a Manager with a single standalone chain and no
// registry chains, so Refresh returns an empty ChangeSet without any HTTP fetch.
func standaloneOnlyManager(t *testing.T) *chain.Manager {
	t.Helper()
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	enabled := true
	bech32 := "mynet"
	slip44 := uint32(118)
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID:      "mynet-1",
			Standalone:   true,
			Enabled:      &enabled,
			Bech32Prefix: &bech32,
			Slip44:       &slip44,
			Endpoints:    &config.EndpointsConfig{GRPC: []string{"localhost:9999"}},
			FeeTokens:    []config.FeeTokenConfig{{Denom: "umynet"}},
			Drip:         config.DripConfig{Anonymous: "1000000umynet", MaxPerAddressPerDay: "10000000umynet"},
		}},
	}
	mgr, err := chain.New(t.Context(), chain.Options{Config: cfg, GasCache: gascache.New(s), MnemonicFn: func() string { return testMnemonic }})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

// minimalChainsYML writes a valid single-chain standalone chains.yml to dir and returns its path.
func minimalChainsYML(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "chains.yml")
	const content = `chains:
  - chain_id: mynet-1
    standalone: true
    enabled: true
    bech32_prefix: mynet
    slip44: 118
    endpoints:
      grpc:
        - localhost:9999
    fee_tokens:
      - denom: umynet
        average_gas_price: "0.025"
    drip:
      anonymous: 1000000umynet
      max_per_address_per_day: 10000000umynet
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write chains.yml: %v", err)
	}
	return path
}

func TestHandler_refresh_standaloneOnly(t *testing.T) {
	mgr := standaloneOnlyManager(t)
	h := New(Deps{RegStore: mgr.Store(), Manager: mgr})

	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/refresh", http.NoBody)
	w := httptest.NewRecorder()
	h.Refresh(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("refresh: got %d, want 200", w.Code)
	}
	var resp struct {
		HotReloaded int `json:"hot_reloaded"`
		Warned      int `json:"warned"`
		Frozen      int `json:"frozen"`
		Removed     int `json:"removed"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HotReloaded != 0 || resp.Warned != 0 || resp.Frozen != 0 || resp.Removed != 0 {
		t.Errorf("expected all-zero changeset for standalone-only manager, got %+v", resp)
	}
}

func TestHandler_refresh_registryError(t *testing.T) {
	fetches := make(chan struct{}, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"chain_id":"mychain-1","chain_name":"mychain","bech32_prefix":"mychain",`+
			`"slip44":118,"network_type":"testnet","key_algos":["secp256k1"],`+
			`"fees":{"fee_tokens":[{"denom":"umychain","average_gas_price":"0.025"}]},`+
			`"apis":{"grpc":[{"address":"localhost:9999"}]}}`)
	}))

	enabled := true
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID: "mychain-1",
			Enabled: &enabled,
			Drip:    config.DripConfig{Anonymous: "1000000umychain", MaxPerAddressPerDay: "10000000umychain"},
		}},
	}
	mgr, err := chain.New(t.Context(), chain.Options{
		Config:          cfg,
		GasCache:        gascache.New(s),
		RegistryBaseURL: srv.URL,
		MnemonicFn:      func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}
	t.Cleanup(mgr.Close)

	// Drain the initial fetch from New().
	select {
	case <-fetches:
	case <-t.Context().Done():
		t.Fatal("initial fetch timed out")
	}

	// Close the server so the next Refresh call fails.
	srv.Close()

	h := New(Deps{RegStore: mgr.Store(), Manager: mgr})
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/registry/refresh", http.NoBody)
	w := httptest.NewRecorder()
	h.Refresh(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("refresh with dead registry: got %d, want 502", w.Code)
	}
}

func TestHandler_reload_configNotFound(t *testing.T) {
	mgr := standaloneOnlyManager(t)
	h := New(Deps{
		RegStore:   mgr.Store(),
		Manager:    mgr,
		ConfigPath: filepath.Join(t.TempDir(), "nonexistent.yml"),
	})

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/reload", http.NoBody)
	w := httptest.NewRecorder()
	h.Reload(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("reload config not found: got %d, want 422", w.Code)
	}
}

func TestHandler_reload(t *testing.T) {
	configPath := minimalChainsYML(t, t.TempDir())
	mgr := standaloneOnlyManager(t)
	h := New(Deps{RegStore: mgr.Store(), Manager: mgr, ConfigPath: configPath})

	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/reload", http.NoBody)
	w := httptest.NewRecorder()
	h.Reload(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("reload: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp["ok"] {
		t.Errorf("reload: expected {ok:true}, got %v", resp)
	}
}

// ----- distributor / gas-cache / chain-status / resume handler tests -----

// newAdminSetup builds a handler backed by a standalone-only manager and a fresh gas cache.
func newAdminSetup(t *testing.T) (*Handler, *chain.Manager, *gascache.Cache) {
	t.Helper()
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	gc := gascache.New(s)
	mgr := standaloneOnlyManager(t)
	h := New(Deps{RegStore: mgr.Store(), Manager: mgr, GasCache: gc})
	return h, mgr, gc
}

// serve sends req through the handler's router and returns the recorder.
func serve(h *Handler, method, path string, body string) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	var req *http.Request
	if bodyReader != nil {
		req = httptest.NewRequestWithContext(context.Background(), method, path, bodyReader)
	} else {
		req = httptest.NewRequestWithContext(context.Background(), method, path, http.NoBody)
	}
	w := httptest.NewRecorder()
	h.Router().ServeHTTP(w, req)
	return w
}

func TestHandler_distributorList_notFound(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodGet, "/distributors/unknown-1", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandler_distributorList_success(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodGet, "/distributors/mynet-1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Distributors []distributorJSON `json:"distributors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Distributors) != 1 {
		t.Fatalf("expected 1 distributor, got %d", len(resp.Distributors))
	}
	if resp.Distributors[0].Index != 1 {
		t.Errorf("index = %d, want 1", resp.Distributors[0].Index)
	}
	if resp.Distributors[0].Status != "healthy" {
		t.Errorf("status = %q, want healthy", resp.Distributors[0].Status)
	}
}

func TestHandler_distributorRefill_notFound(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodPost, "/distributors/unknown-1/refill", "{}")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandler_distributorRefill_badBody(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodPost, "/distributors/mynet-1/refill", "not json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandler_distributorRefill_all(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	// Refill all: gRPC will fail (no real node), so results have ok=false.
	// The handler itself should still return 200 with per-distributor results.
	w := serve(h, http.MethodPost, "/distributors/mynet-1/refill", "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Results []refillResult `json:"results"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Index != 1 {
		t.Errorf("result index = %d, want 1", resp.Results[0].Index)
	}
}

func TestHandler_distributorRefill_specificIndex(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodPost, "/distributors/mynet-1/refill", `{"index":1}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Results []refillResult `json:"results"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
}

func TestHandler_gasCacheGet_chainNotFound(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodGet, "/chains/unknown-1/gas-cache", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandler_gasCacheGet_cacheMiss(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodGet, "/chains/mynet-1/gas-cache", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 when no cache entry", w.Code)
	}
}

func TestHandler_gasCacheGet_success(t *testing.T) {
	h, _, gc := newAdminSetup(t)
	if err := gc.RecordSuccess(t.Context(), "mynet-1", tx.MsgTypeSend, 150_000, 1, "umynet", "0.025"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	w := serve(h, http.MethodGet, "/chains/mynet-1/gas-cache", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var row gascache.GasCacheRow
	if err := json.NewDecoder(w.Body).Decode(&row); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if row.BaseGas != 150_000 {
		t.Errorf("BaseGas = %d, want 150000", row.BaseGas)
	}
	if row.FeeDenom != "umynet" {
		t.Errorf("FeeDenom = %q, want umynet", row.FeeDenom)
	}
}

func TestHandler_gasCacheReset_notFound(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodPost, "/chains/unknown-1/gas-cache/reset", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandler_gasCacheReset_success(t *testing.T) {
	h, _, gc := newAdminSetup(t)
	if err := gc.RecordSuccess(t.Context(), "mynet-1", tx.MsgTypeSend, 150_000, 1, "umynet", "0.025"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	w := serve(h, http.MethodPost, "/chains/mynet-1/gas-cache/reset", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	// Verify row is gone.
	_, found, err := gc.Read(t.Context(), "mynet-1")
	if err != nil {
		t.Fatalf("Read after reset: %v", err)
	}
	if found {
		t.Error("expected cache entry deleted after reset")
	}
}

func TestHandler_chainStatus_notFound(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodGet, "/chains/unknown-1/status", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandler_chainStatus_success(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodGet, "/chains/mynet-1/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp chainStatusJSON
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Suspended {
		t.Error("expected chain not suspended initially")
	}
	if resp.MultiSendDisabled {
		t.Error("expected multisend not disabled initially")
	}
}

func TestHandler_chainResume_notFound(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodPost, "/chains/unknown-1/resume", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandler_chainResume_notSuspended(t *testing.T) {
	h, _, _ := newAdminSetup(t)
	w := serve(h, http.MethodPost, "/chains/mynet-1/resume", "")
	if w.Code != http.StatusConflict {
		t.Errorf("got %d, want 409 (chain not suspended)", w.Code)
	}
}

func TestHandler_chainResume_success(t *testing.T) {
	h, mgr, _ := newAdminSetup(t)
	c, ok := mgr.GetChain("mynet-1")
	if !ok {
		t.Fatal("chain mynet-1 not found in manager")
	}
	c.Suspend(errors.New("forced for test"))

	w := serve(h, http.MethodPost, "/chains/mynet-1/resume", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp["ok"] {
		t.Error("expected {ok:true}")
	}
	if c.ChainStatus().Suspended {
		t.Error("chain should no longer be suspended after resume")
	}
}
