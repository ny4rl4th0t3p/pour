package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
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

func TestInfo_IBCChannelCount(t *testing.T) {
	channel := chainregistry.IBCChannel{
		ChainNameA: "cosmoshub", ChainNameB: "osmosis",
		ChannelA: "channel-141", ChannelB: "channel-0",
		PortA: "transfer", PortB: "transfer",
		Status: "live", Preferred: true,
	}
	src := &stubChainSource{
		snaps: map[string]chain.ChainSnapshot{
			"osmosis-1":   {Info: &chainregistry.ChainInfo{ChainID: "osmosis-1", ChainName: "osmosis"}},
			"cosmoshub-4": {Info: &chainregistry.ChainInfo{ChainID: "cosmoshub-4", ChainName: "cosmoshub"}},
		},
		channels: map[string][]chainregistry.IBCChannel{
			"osmosis":   {channel},
			"cosmoshub": {channel},
		},
		allChannels: []chainregistry.IBCChannel{channel},
	}
	h := New(Deps{Source: src, Version: "test"})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/info", http.NoBody)
	h.Info(w, r)

	var resp pourapi.InfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IBCChannelCount != 1 {
		t.Errorf("ibc_channel_count: got %d, want 1", resp.IBCChannelCount)
	}
}

func TestChainDetail_IBCChannels(t *testing.T) {
	src := &stubChainSource{
		snaps: map[string]chain.ChainSnapshot{
			"osmosis-1": {
				Info: &chainregistry.ChainInfo{
					ChainID:     "osmosis-1",
					ChainName:   "osmosis",
					LastChanged: time.Time{},
				},
				Drip: chainregistry.DripPolicy{Anonymous: "1000000uosmo", MaxPerAddressPerDay: "50000000uosmo"},
			},
		},
		channels: map[string][]chainregistry.IBCChannel{
			"osmosis": {
				{
					ChainNameA: "cosmoshub", ChainNameB: "osmosis",
					ChannelA: "channel-141", ChannelB: "channel-0",
					PortA: "transfer", PortB: "transfer",
					Status: "live", Preferred: true,
				},
			},
		},
	}
	h := New(Deps{Source: src, Version: "test"})
	router := h.testRouter()
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/chains/osmosis-1", http.NoBody)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.ChainDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.IBCChannels) != 1 {
		t.Fatalf("ibc_channels: got %d, want 1", len(resp.IBCChannels))
	}
	ch := resp.IBCChannels[0]
	if ch.PeerChainName != "cosmoshub" {
		t.Errorf("peer_chain_name: got %q, want cosmoshub", ch.PeerChainName)
	}
	if ch.ChannelID != "channel-0" {
		t.Errorf("channel_id: got %q, want channel-0", ch.ChannelID)
	}
	if ch.PeerChannelID != "channel-141" {
		t.Errorf("peer_channel_id: got %q, want channel-141", ch.PeerChannelID)
	}
	if ch.PortID != "transfer" {
		t.Errorf("port_id: got %q, want transfer", ch.PortID)
	}
	if ch.Status != "live" {
		t.Errorf("status: got %q, want live", ch.Status)
	}
	if !ch.Preferred {
		t.Error("preferred: want true")
	}
}

func TestChainDetail_NoIBCChannels(t *testing.T) {
	h := New(Deps{Source: testSource, Version: "test"})
	router := h.testRouter()
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/chains/osmosis-1", http.NoBody)
	router.ServeHTTP(w, r)

	var resp pourapi.ChainDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IBCChannels == nil {
		t.Error("ibc_channels: want empty slice, got nil (would serialize as null)")
	}
	if len(resp.IBCChannels) != 0 {
		t.Errorf("ibc_channels: got %d entries, want 0", len(resp.IBCChannels))
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
