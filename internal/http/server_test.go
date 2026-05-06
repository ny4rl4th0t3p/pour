package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/abuse"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	"github.com/ny4rl4th0t3p/pour/internal/http/handlers"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

const testServerMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// ----- test doubles -----

type fixedBroadcaster struct{}

func (fixedBroadcaster) BuildAndBroadcast(_ context.Context, _ tx.SendRequest) (*tx.BroadcastResult, error) {
	return &tx.BroadcastResult{TxHash: "TESTHASH", Height: 1}, nil
}

// passthroughAdmitter always admits with an anonymous decision using the chain's configured drip.
type passthroughAdmitter struct{}

func (*passthroughAdmitter) Admit(_ context.Context, _ *nethttp.Request, _ *pourapi.PourRequest, cc abuse.ChainContext) (*abuse.Decision, error) {
	return &abuse.Decision{Mechanism: abuse.MechanismAnonymous, DripCoin: cc.DripAnonymous}, nil
}

// countingAdmitter enforces a per-chain request limit, mirroring IP rate-limit behavior.
type countingAdmitter struct {
	mu     sync.Mutex
	counts map[string]int
	limit  int
}

func newCountingAdmitter(limit int) *countingAdmitter {
	return &countingAdmitter{counts: make(map[string]int), limit: limit}
}

func (a *countingAdmitter) Admit(_ context.Context, _ *nethttp.Request, _ *pourapi.PourRequest, cc abuse.ChainContext) (*abuse.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.counts[cc.ChainID]++
	if a.counts[cc.ChainID] > a.limit {
		return nil, &ratelimit.ErrRateLimitExceeded{RetryAfter: time.Hour}
	}
	return &abuse.Decision{Mechanism: abuse.MechanismAnonymous, DripCoin: cc.DripAnonymous}, nil
}

// newMultiChainSrv creates a test server with two standalone chains (osmosis-1, cosmos-1).
func newMultiChainSrv(t *testing.T, limitPerChain int) *httptest.Server {
	t.Helper()
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	trueVal := true
	osmoPrefix := "osmo"
	cosmosPrefix := "cosmos"
	slip44 := uint32(118)
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{
			{
				ChainID:      "osmosis-1",
				Standalone:   true,
				Enabled:      &trueVal,
				Bech32Prefix: &osmoPrefix,
				Slip44:       &slip44,
				Endpoints:    &config.EndpointsConfig{GRPC: []string{"localhost:9999"}},
				FeeTokens:    []config.FeeTokenConfig{{Denom: "uosmo"}},
				Drip:         config.DripConfig{Anonymous: "1000000uosmo", MaxPerAddressPerDay: "50000000uosmo"},
				BatchWindow:  "0", // sync mode so tests use the injected broadcaster
			},
			{
				ChainID:      "cosmos-1",
				Standalone:   true,
				Enabled:      &trueVal,
				Bech32Prefix: &cosmosPrefix,
				Slip44:       &slip44,
				Endpoints:    &config.EndpointsConfig{GRPC: []string{"localhost:9999"}},
				FeeTokens:    []config.FeeTokenConfig{{Denom: "uatom"}},
				Drip:         config.DripConfig{Anonymous: "1000000uatom", MaxPerAddressPerDay: "50000000uatom"},
				BatchWindow:  "0", // sync mode so tests use the injected broadcaster
			},
		},
	}
	mgr, err := chain.New(t.Context(), chain.Options{Config: cfg, GasCache: gascache.New(s), MnemonicFn: func() string { return testServerMnemonic }})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}
	t.Cleanup(mgr.Close)

	srv, err := New(Deps{
		Manager: mgr,
		Serve:   &config.ServeConfig{Listen: ":0"},
		Store:   s,
		Gate:    newCountingAdmitter(limitPerChain),
		Broadcasters: map[string]handlers.Broadcaster{
			"osmosis-1": fixedBroadcaster{},
			"cosmos-1":  fixedBroadcaster{},
		},
		Version: "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return httptest.NewServer(srv.router)
}

// ----- helpers -----

func newTestSrv(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := store.New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	enabled := true
	bech32 := "osmo"
	slip44 := uint32(118)
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID:      "osmosis-1",
			Standalone:   true,
			Enabled:      &enabled,
			Bech32Prefix: &bech32,
			Slip44:       &slip44,
			Endpoints:    &config.EndpointsConfig{GRPC: []string{"localhost:9999"}},
			FeeTokens:    []config.FeeTokenConfig{{Denom: "uosmo"}},
			Drip:         config.DripConfig{Anonymous: "1000000uosmo", MaxPerAddressPerDay: "50000000uosmo"},
			BatchWindow:  "0", // sync mode so tests use the injected broadcaster
		}},
	}
	mgr, err := chain.New(t.Context(), chain.Options{
		Config:     cfg,
		GasCache:   gascache.New(s),
		MnemonicFn: func() string { return testServerMnemonic },
	})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}
	t.Cleanup(mgr.Close)

	srv, err := New(Deps{
		Manager:      mgr,
		Serve:        &config.ServeConfig{Listen: ":0"},
		Store:        s,
		Gate:         &passthroughAdmitter{},
		Broadcasters: map[string]handlers.Broadcaster{"osmosis-1": fixedBroadcaster{}},
		Version:      "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return httptest.NewServer(srv.router)
}

func get(t *testing.T, srv *httptest.Server, path string) *nethttp.Response {
	t.Helper()
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, srv.URL+path, nethttp.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func post(t *testing.T, srv *httptest.Server, body string) *nethttp.Response {
	t.Helper()
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPost, srv.URL+"/v1/pour",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// ----- routing tests -----

func TestServer_health(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := get(t, srv, "/health")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var h pourapi.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status: got %q, want ok", h.Status)
	}
}

func TestServer_info(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := get(t, srv, "/v1/info")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var info pourapi.InfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Version != "test" {
		t.Errorf("version: got %q, want test", info.Version)
	}
}

func TestServer_chains(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := get(t, srv, "/v1/chains")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var chains pourapi.ChainsResponse
	if err := json.NewDecoder(resp.Body).Decode(&chains); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(chains.Chains) != 1 || chains.Chains[0].ChainID != "osmosis-1" {
		t.Errorf("chains: got %+v", chains.Chains)
	}
}

func TestServer_pour_badJSON(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := post(t, srv, "not json")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestServer_pour_success(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := post(t, srv, `{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`)
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var p pourapi.PourResponse
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.TxHash != "TESTHASH" {
		t.Errorf("tx_hash: got %q, want TESTHASH", p.TxHash)
	}
}

func TestServer_ui_root(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := get(t, srv, "/")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: got %q, want text/html prefix", ct)
	}
}

func TestServer_ui_altchaJS(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := get(t, srv, "/altcha.min.js")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Error("altcha.min.js: empty body")
	}
}

func TestServer_requestID_middleware(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := get(t, srv, "/health")
	defer resp.Body.Close()

	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header missing: RequestID middleware not applied")
	}
}

// ----- multi-chain tests -----

func TestServer_multichain_pour(t *testing.T) {
	srv := newMultiChainSrv(t, 10)
	defer srv.Close()

	for _, tc := range []struct {
		chainID string
		addr    string
	}{
		{"osmosis-1", "osmo1abc123defg"},
		{"cosmos-1", "cosmos1abc123defg"},
	} {
		resp := post(t, srv,
			`{"chain_id":"`+tc.chainID+`","address":"`+tc.addr+`"}`)
		if resp.StatusCode != nethttp.StatusOK {
			resp.Body.Close()
			t.Errorf("%s: got %d, want 200", tc.chainID, resp.StatusCode)
			continue
		}
		var p pourapi.PourResponse
		err := json.NewDecoder(resp.Body).Decode(&p)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: decode: %v", tc.chainID, err)
		}
		if p.TxHash != "TESTHASH" {
			t.Errorf("%s: tx_hash: got %q, want TESTHASH", tc.chainID, p.TxHash)
		}
	}
}

func TestServer_multichain_ratelimit_independent(t *testing.T) {
	// limit=1 per chain; exhausting osmosis-1's limit must not block cosmos-1.
	srv := newMultiChainSrv(t, 1)
	defer srv.Close()

	// First pour on osmosis-1: succeeds.
	r1 := post(t, srv, `{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`)
	defer r1.Body.Close()
	if r1.StatusCode != nethttp.StatusOK {
		t.Fatalf("osmosis-1 first pour: got %d, want 200", r1.StatusCode)
	}

	// Second pour on osmosis-1: rate limited.
	r2 := post(t, srv, `{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`)
	defer r2.Body.Close()
	if r2.StatusCode != nethttp.StatusTooManyRequests {
		t.Fatalf("osmosis-1 second pour: got %d, want 429", r2.StatusCode)
	}

	// First pour on cosmos-1: must succeed — limit is scoped per chain ID.
	r3 := post(t, srv, `{"chain_id":"cosmos-1","address":"cosmos1abc123defg"}`)
	defer r3.Body.Close()
	if r3.StatusCode != nethttp.StatusOK {
		t.Fatalf("cosmos-1 pour: got %d, want 200 (limits are chain-independent)", r3.StatusCode)
	}
}
