package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

func TestHealth(t *testing.T) {
	h := New(Deps{Source: testSource, Version: "test"})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	h.Health(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status: got %q, want ok", resp.Status)
	}
}

func TestInfo(t *testing.T) {
	h := New(Deps{Source: testSource, Version: "v0.2.0-test", RegistryRefreshMode: "live"})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/info", http.NoBody)
	h.Info(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.InfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != "v0.2.0-test" {
		t.Errorf("version: got %q, want v0.2.0-test", resp.Version)
	}
	if resp.RegistryRefreshMode != "live" {
		t.Errorf("refresh_mode: got %q, want live", resp.RegistryRefreshMode)
	}
	if resp.RegistryLastFetched != "" {
		t.Errorf("registry_last_fetched: stub source returns zero time, want empty string, got %q", resp.RegistryLastFetched)
	}
	// Default AbuseCfg — all mechanism flags false.
	if resp.Abuse.PoWEnabled {
		t.Error("pow_enabled: want false for zero AbuseCfg")
	}
	if resp.Abuse.APIKeysEnabled {
		t.Error("api_keys_enabled: want false for zero AbuseCfg")
	}
	if resp.Abuse.SignatureChallengeEnabled {
		t.Error("signature_challenge_enabled: want false for zero AbuseCfg")
	}
}

func TestInfo_abuseFlags(t *testing.T) {
	h := New(Deps{
		Source:  testSource,
		Version: "test",
		AbuseCfg: config.AbuseConfig{
			PoW:                config.AbusePoWConfig{Enabled: true},
			APIKeys:            config.AbuseAPIKeysConfig{Enabled: true},
			SignatureChallenge: config.AbuseSignatureChallengeConfig{Enabled: true},
		},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/info", http.NoBody)
	h.Info(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.InfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Abuse.PoWEnabled {
		t.Error("pow_enabled: want true")
	}
	if !resp.Abuse.APIKeysEnabled {
		t.Error("api_keys_enabled: want true")
	}
	if !resp.Abuse.SignatureChallengeEnabled {
		t.Error("signature_challenge_enabled: want true")
	}
}

func TestChains(t *testing.T) {
	h := New(Deps{Source: testSource, Version: "test"})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/chains", http.NoBody)
	h.Chains(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.ChainsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Chains) != 1 {
		t.Fatalf("chains: got %d, want 1", len(resp.Chains))
	}
	if resp.Chains[0].ChainID != "osmosis-1" {
		t.Errorf("chain_id: got %q, want osmosis-1", resp.Chains[0].ChainID)
	}
	if resp.Chains[0].DripAmount != "1000000uosmo" {
		t.Errorf("drip_amount: got %q, want 1000000uosmo", resp.Chains[0].DripAmount)
	}
}
