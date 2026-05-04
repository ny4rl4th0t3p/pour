package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/http/handlers"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// ----- test doubles -----

type fixedBroadcaster struct{}

func (fixedBroadcaster) BuildAndBroadcast(_ context.Context, _ tx.SendRequest) (*tx.BroadcastResult, error) {
	return &tx.BroadcastResult{TxHash: "TESTHASH", Height: 1}, nil
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
	grpcEP := "grpc.osmosis.zone:9090"
	chains := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID:      "osmosis-1",
			Enabled:      &enabled,
			Bech32Prefix: &bech32,
			Slip44:       &slip44,
			Endpoints:    &config.EndpointsConfig{GRPC: []string{grpcEP}},
			Drip:         config.DripConfig{Anonymous: "1000000uosmo"},
		}},
	}
	srv, err := New(Deps{
		ChainsConfig: chains,
		Serve:        &config.ServeConfig{Listen: ":0"},
		Store:        s,
		Limiter:      ratelimit.New(s, 10, time.Hour),
		Broadcasters: map[string]handlers.Broadcaster{"osmosis-1": fixedBroadcaster{}},
		Mnemonic:     "test",
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

func post(t *testing.T, srv *httptest.Server, path, body string) *nethttp.Response {
	t.Helper()
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPost, srv.URL+path,
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

	resp := post(t, srv, "/v1/pour", "not json")
	defer resp.Body.Close()

	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestServer_pour_success(t *testing.T) {
	srv := newTestSrv(t)
	defer srv.Close()

	resp := post(t, srv, "/v1/pour", `{"chain_id":"osmosis-1","address":"osmo1abc123defg"}`)
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
