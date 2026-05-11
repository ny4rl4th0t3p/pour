package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

const validYAML = `
abuse:
  ip_rate_limit:
    requests_per_window: 10
    window: 1h

chains:
  - chain_id: osmosis-1
    enabled: true
    bech32_prefix: osmo
    slip44: 118
    endpoints:
      grpc:
        - grpc.osmosis.zone:9090
    fee_tokens:
      - denom: uosmo
        average_gas_price: "0.025"
        low_gas_price: "0.0025"
    drip:
      anonymous: "1000000uosmo"
      max_per_address_per_day: "50000000uosmo"
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chains.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadChains_valid(t *testing.T) {
	cfg, err := LoadChains(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}

	if cfg.Abuse.IPRateLimit.RequestsPerWindow != 10 {
		t.Errorf("RequestsPerWindow: got %d, want 10", cfg.Abuse.IPRateLimit.RequestsPerWindow)
	}
	if cfg.Abuse.IPRateLimit.Window != "1h" {
		t.Errorf("Window: got %q, want 1h", cfg.Abuse.IPRateLimit.Window)
	}
	if len(cfg.Chains) != 1 {
		t.Fatalf("Chains len: got %d, want 1", len(cfg.Chains))
	}

	c := cfg.Chains[0]
	if c.ChainID != "osmosis-1" {
		t.Errorf("ChainID: got %s, want osmosis-1", c.ChainID)
	}
	if !c.IsEnabled() {
		t.Error("IsEnabled: want true")
	}
	if c.Bech32Prefix == nil || *c.Bech32Prefix != "osmo" {
		t.Errorf("Bech32Prefix: got %v, want osmo", c.Bech32Prefix)
	}
	if c.Slip44 == nil || *c.Slip44 != 118 {
		t.Errorf("Slip44: got %v, want 118", c.Slip44)
	}
	if c.Endpoints == nil || len(c.Endpoints.GRPC) != 1 || c.Endpoints.GRPC[0] != "grpc.osmosis.zone:9090" {
		t.Errorf("Endpoints.GRPC: got %v", c.Endpoints)
	}
	if len(c.FeeTokens) != 1 || c.FeeTokens[0].Denom != "uosmo" {
		t.Errorf("FeeTokens: got %v", c.FeeTokens)
	}
	if c.Drip.Anonymous != "1000000uosmo" {
		t.Errorf("Drip.Anonymous: got %s, want 1000000uosmo", c.Drip.Anonymous)
	}
	if c.Drip.MaxPerAddressPerDay != "50000000uosmo" {
		t.Errorf("Drip.MaxPerAddressPerDay: got %s", c.Drip.MaxPerAddressPerDay)
	}
}

func TestLoadChains_missingAnonymous(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    enabled: true
    drip:
      max_per_address_per_day: "50000000uosmo"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for missing drip.anonymous")
	}
}

func TestLoadChains_missingMaxPerDay(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for missing drip.max_per_address_per_day")
	}
}

func TestLoadChains_disabledChain(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    enabled: false
`
	_, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("disabled chain with no drip fields should not error: %v", err)
	}
}

func TestLoadChains_fileNotFound(t *testing.T) {
	_, err := LoadChains(filepath.Join(t.TempDir(), "nonexistent.yml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadChains_invalidAdminCIDR(t *testing.T) {
	yml := `
admin:
  allowed_cidrs:
    - "not-a-cidr"
chains:
  - chain_id: osmosis-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestLoadChains_invalidBlockTime(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    block_time: "not-a-duration"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid block_time")
	}
}

func TestLoadChains_standalone_valid(t *testing.T) {
	yml := `
chains:
  - chain_id: mynet-1
    standalone: true
    enabled: true
    bech32_prefix: mynet
    slip44: 118
    endpoints:
      grpc:
        - grpc.mynet.example:9090
    fee_tokens:
      - denom: umynet
        average_gas_price: "0.025"
    drip:
      anonymous: "1000000umynet"
      max_per_address_per_day: "50000000umynet"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("valid standalone chain should not error: %v", err)
	}
}

func TestLoadChains_standalone_missingBech32(t *testing.T) {
	yml := `
chains:
  - chain_id: mynet-1
    standalone: true
    slip44: 118
    endpoints:
      grpc:
        - grpc.mynet.example:9090
    fee_tokens:
      - denom: umynet
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for standalone chain missing bech32_prefix")
	}
}

func TestLoadChains_standalone_missingGRPC(t *testing.T) {
	yml := `
chains:
  - chain_id: mynet-1
    standalone: true
    bech32_prefix: mynet
    slip44: 118
    fee_tokens:
      - denom: umynet
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for standalone chain missing endpoints.grpc")
	}
}

func TestLoadChains_standalone_RESTOnly(t *testing.T) {
	yml := `
chains:
  - chain_id: mynet-1
    standalone: true
    enabled: true
    bech32_prefix: mynet
    slip44: 118
    endpoints:
      rest:
        - https://lcd.mynet.example
    fee_tokens:
      - denom: umynet
        average_gas_price: "0.025"
    drip:
      anonymous: "1000000umynet"
      max_per_address_per_day: "50000000umynet"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("REST-only standalone chain should not error: %v", err)
	}
}

func TestToOverrideSet_registryChain(t *testing.T) {
	cfg, err := LoadChains(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}

	ov, err := cfg.ToOverrideSet()
	if err != nil {
		t.Fatalf("ToOverrideSet: %v", err)
	}
	co, ok := ov.Chains["osmosis-1"]
	if !ok {
		t.Fatal("expected osmosis-1 in override set")
	}
	if co.Bech32Prefix == nil || *co.Bech32Prefix != "osmo" {
		t.Errorf("Bech32Prefix: got %v, want osmo", co.Bech32Prefix)
	}
	if co.Endpoints == nil || len(co.Endpoints.GRPC) != 1 {
		t.Errorf("Endpoints.GRPC: got %v", co.Endpoints)
	}
	if len(co.FeeTokens) != 1 || co.FeeTokens[0].Denom != "uosmo" {
		t.Errorf("FeeTokens: got %v", co.FeeTokens)
	}
}

func TestToOverrideSet_standaloneExcluded(t *testing.T) {
	yml := `
chains:
  - chain_id: mynet-1
    standalone: true
    bech32_prefix: mynet
    slip44: 118
    endpoints:
      grpc:
        - grpc.mynet.example:9090
    fee_tokens:
      - denom: umynet
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}

	ov, err := cfg.ToOverrideSet()
	if err != nil {
		t.Fatalf("ToOverrideSet: %v", err)
	}
	if _, ok := ov.Chains["mynet-1"]; ok {
		t.Error("standalone chain should not appear in override set")
	}
}

func TestToStandaloneInfos(t *testing.T) {
	yml := `
chains:
  - chain_id: mynet-1
    standalone: true
    bech32_prefix: mynet
    slip44: 118
    network_type: testnet
    endpoints:
      grpc:
        - grpc.mynet.example:9090
      rpc:
        - https://rpc.mynet.example
    fee_tokens:
      - denom: umynet
        average_gas_price: "0.025"
        low_gas_price: "0.001"
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}

	infos, err := cfg.ToStandaloneInfos()
	if err != nil {
		t.Fatalf("ToStandaloneInfos: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 standalone info, got %d", len(infos))
	}
	info := infos[0]
	if info.ChainID != "mynet-1" {
		t.Errorf("ChainID: got %q", info.ChainID)
	}
	if info.Bech32Prefix != "mynet" {
		t.Errorf("Bech32Prefix: got %q", info.Bech32Prefix)
	}
	if info.Slip44 != 118 {
		t.Errorf("Slip44: got %d", info.Slip44)
	}
	if info.NetworkType != chainregistry.NetworkTypeTestnet {
		t.Errorf("NetworkType: got %v", info.NetworkType)
	}
	if len(info.Endpoints.GRPC) != 1 || info.Endpoints.GRPC[0].URL != "grpc.mynet.example:9090" {
		t.Errorf("Endpoints.GRPC: got %v", info.Endpoints.GRPC)
	}
	if len(info.Endpoints.RPC) != 1 {
		t.Errorf("Endpoints.RPC: got %v", info.Endpoints.RPC)
	}
	if len(info.FeeTokens) != 1 || info.FeeTokens[0].Denom != "umynet" {
		t.Errorf("FeeTokens: got %v", info.FeeTokens)
	}
	if info.FeeTokens[0].AverageGasPrice.String() != "0.025" {
		t.Errorf("AverageGasPrice: got %v", info.FeeTokens[0].AverageGasPrice)
	}
}

func TestToStandaloneInfos_registryExcluded(t *testing.T) {
	cfg, err := LoadChains(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	infos, err := cfg.ToStandaloneInfos()
	if err != nil {
		t.Fatalf("ToStandaloneInfos: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("registry chain should not appear in standalone infos, got %v", infos)
	}
}

func TestWindowDuration_invalid(t *testing.T) {
	_, err := IPRateLimitConfig{Window: "notaduration"}.WindowDuration()
	if err == nil {
		t.Fatal("expected error for invalid window duration")
	}
}

func TestWindowDuration_nonPositive(t *testing.T) {
	_, err := IPRateLimitConfig{Window: "-1m"}.WindowDuration()
	if err == nil {
		t.Fatal("expected error for non-positive window")
	}
}

func TestRefreshDuration_empty(t *testing.T) {
	d, err := RegistryConfig{}.RefreshDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 6*time.Hour {
		t.Errorf("got %v, want 6h", d)
	}
}

func TestRefreshDuration_valid(t *testing.T) {
	d, err := RegistryConfig{RefreshInterval: "12h"}.RefreshDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 12*time.Hour {
		t.Errorf("got %v, want 12h", d)
	}
}

func TestRefreshDuration_invalid(t *testing.T) {
	_, err := RegistryConfig{RefreshInterval: "notaduration"}.RefreshDuration()
	if err == nil {
		t.Fatal("expected error for invalid refresh interval")
	}
}

func TestRefreshDuration_nonPositive(t *testing.T) {
	_, err := RegistryConfig{RefreshInterval: "-6h"}.RefreshDuration()
	if err == nil {
		t.Fatal("expected error for non-positive refresh interval")
	}
}

func TestEnabledRegistryChainIDs(t *testing.T) {
	enabled := true
	disabled := false
	cfg := &ChainsConfig{
		Chains: []ChainConfig{
			{ChainID: "osmosis-1", Enabled: &enabled},
			{ChainID: "cosmos-1", Enabled: &disabled},
			{ChainID: "mynet-1", Standalone: true, Enabled: &enabled},
		},
	}
	ids := cfg.EnabledRegistryChainIDs()
	if len(ids) != 1 || ids[0] != "osmosis-1" {
		t.Errorf("EnabledRegistryChainIDs: got %v, want [osmosis-1]", ids)
	}
}

func TestToChainInfo_basic(t *testing.T) {
	bech32 := "osmo"
	slip44 := uint32(118)
	cfg := &ChainConfig{
		ChainID:      "osmosis-1",
		Bech32Prefix: &bech32,
		Slip44:       &slip44,
	}
	info, err := cfg.ToChainInfo()
	if err != nil {
		t.Fatalf("ToChainInfo: %v", err)
	}
	if info.ChainID != "osmosis-1" {
		t.Errorf("ChainID: got %q", info.ChainID)
	}
	if info.Bech32Prefix != "osmo" {
		t.Errorf("Bech32Prefix: got %q", info.Bech32Prefix)
	}
	if info.Slip44 != 118 {
		t.Errorf("Slip44: got %d", info.Slip44)
	}
}

func TestToChainInfo_withBlockTime(t *testing.T) {
	bt := "6s"
	cfg := &ChainConfig{ChainID: "mynet-1", BlockTime: &bt}
	info, err := cfg.ToChainInfo()
	if err != nil {
		t.Fatalf("ToChainInfo: %v", err)
	}
	if info.BlockTime != 6*time.Second {
		t.Errorf("BlockTime: got %v, want 6s", info.BlockTime)
	}
}

func TestToChainInfo_invalidBlockTime(t *testing.T) {
	bt := "notaduration"
	cfg := &ChainConfig{ChainID: "mynet-1", BlockTime: &bt}
	if _, err := cfg.ToChainInfo(); err == nil {
		t.Fatal("expected error for invalid block_time")
	}
}

func TestToChainInfo_withKeyAlgoAndNetworkType(t *testing.T) {
	ka := "secp256k1"
	nt := "testnet"
	cfg := &ChainConfig{ChainID: "mynet-1", KeyAlgo: &ka, NetworkType: &nt}
	info, err := cfg.ToChainInfo()
	if err != nil {
		t.Fatalf("ToChainInfo: %v", err)
	}
	if string(info.KeyAlgo) != "secp256k1" {
		t.Errorf("KeyAlgo: got %q", info.KeyAlgo)
	}
	if string(info.NetworkType) != "testnet" {
		t.Errorf("NetworkType: got %q", info.NetworkType)
	}
}

// ----- DistributorCount -----

func TestDistributorCount_zero(t *testing.T) {
	c := &ChainConfig{}
	if got := c.DistributorCount(); got != 1 {
		t.Errorf("zero: got %d, want 1", got)
	}
}

func TestDistributorCount_negative(t *testing.T) {
	c := &ChainConfig{Distributors: -1}
	if got := c.DistributorCount(); got != 1 {
		t.Errorf("negative: got %d, want 1", got)
	}
}

func TestDistributorCount_explicit(t *testing.T) {
	c := &ChainConfig{Distributors: 3}
	if got := c.DistributorCount(); got != 3 {
		t.Errorf("explicit: got %d, want 3", got)
	}
}

// ----- BatchWindowDuration -----

func TestBatchWindowDuration_empty(t *testing.T) {
	c := &ChainConfig{ChainID: "x"}
	d, err := c.BatchWindowDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 5*time.Second {
		t.Errorf("empty: got %v, want 5s", d)
	}
}

func TestBatchWindowDuration_zero(t *testing.T) {
	c := &ChainConfig{ChainID: "x", BatchWindow: "0"}
	d, err := c.BatchWindowDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 0 {
		t.Errorf("zero: got %v, want 0", d)
	}
}

func TestBatchWindowDuration_valid(t *testing.T) {
	c := &ChainConfig{ChainID: "x", BatchWindow: "10s"}
	d, err := c.BatchWindowDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 10*time.Second {
		t.Errorf("valid: got %v, want 10s", d)
	}
}

func TestBatchWindowDuration_invalid(t *testing.T) {
	c := &ChainConfig{ChainID: "x", BatchWindow: "notaduration"}
	if _, err := c.BatchWindowDuration(); err == nil {
		t.Fatal("expected error for invalid batch_window")
	}
}

func TestBatchWindowDuration_negative(t *testing.T) {
	c := &ChainConfig{ChainID: "x", BatchWindow: "-1s"}
	if _, err := c.BatchWindowDuration(); err == nil {
		t.Fatal("expected error for negative batch_window")
	}
}

// ----- MaxRecipientsPerBatchOrDefault -----

func TestMaxRecipientsPerBatchOrDefault_zero(t *testing.T) {
	c := &ChainConfig{}
	if got := c.MaxRecipientsPerBatchOrDefault(); got != DefaultMaxRecipientsPerBatch {
		t.Errorf("zero: got %d, want %d", got, DefaultMaxRecipientsPerBatch)
	}
}

func TestMaxRecipientsPerBatchOrDefault_explicit(t *testing.T) {
	c := &ChainConfig{MaxRecipientsPerBatch: 50}
	if got := c.MaxRecipientsPerBatchOrDefault(); got != 50 {
		t.Errorf("explicit: got %d, want 50", got)
	}
}

// ----- MaxQueueDepthOrDefault -----

func TestMaxQueueDepthOrDefault_zero(t *testing.T) {
	c := &ChainConfig{}
	if got := c.MaxQueueDepthOrDefault(); got != DefaultMaxQueueDepth {
		t.Errorf("zero: got %d, want %d", got, DefaultMaxQueueDepth)
	}
}

func TestMaxQueueDepthOrDefault_explicit(t *testing.T) {
	c := &ChainConfig{MaxQueueDepth: 200}
	if got := c.MaxQueueDepthOrDefault(); got != 200 {
		t.Errorf("explicit: got %d, want 200", got)
	}
}

// ----- LoadChains concurrency field validation -----

func TestLoadChains_invalidBatchWindow(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    batch_window: "notaduration"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid batch_window")
	}
}

func TestLoadChains_negativeBatchWindow(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    batch_window: "-5s"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for negative batch_window")
	}
}

func TestLoadChains_negativeDistributors(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    distributors: -1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for negative distributors")
	}
}

func TestLoadChains_invalidRefillThreshold(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    refill_threshold: "notacoin"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid refill_threshold")
	}
}

func TestLoadChains_concurrencyFields(t *testing.T) {
	yml := `
chains:
  - chain_id: osmosis-1
    enabled: true
    distributors: 3
    batch_window: "5s"
    max_recipients_per_batch: 50
    max_queue_depth: 200
    refill_threshold: "5000000uosmo"
    drip:
      anonymous: "1000000uosmo"
      max_per_address_per_day: "50000000uosmo"
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	c := cfg.Chains[0]
	if c.DistributorCount() != 3 {
		t.Errorf("DistributorCount: got %d, want 3", c.DistributorCount())
	}
	d, err := c.BatchWindowDuration()
	if err != nil || d != 5*time.Second {
		t.Errorf("BatchWindowDuration: got %v %v, want 5s nil", d, err)
	}
	if c.MaxRecipientsPerBatchOrDefault() != 50 {
		t.Errorf("MaxRecipientsPerBatch: got %d, want 50", c.MaxRecipientsPerBatchOrDefault())
	}
	if c.MaxQueueDepthOrDefault() != 200 {
		t.Errorf("MaxQueueDepth: got %d, want 200", c.MaxQueueDepthOrDefault())
	}
	if c.RefillThreshold != "5000000uosmo" {
		t.Errorf("RefillThreshold: got %q", c.RefillThreshold)
	}
}

// ----- AbuseConfig defaults -----

func TestAbuseDefaults_allOmitted(t *testing.T) {
	cfg, err := LoadChains(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	ab := cfg.Abuse
	if ab.PoW.Enabled {
		t.Error("PoW.Enabled: want false when omitted")
	}
	if ab.PoW.Difficulty != "medium" {
		t.Errorf("PoW.Difficulty: got %q, want medium", ab.PoW.Difficulty)
	}
	if ab.APIKeys.Enabled {
		t.Error("APIKeys.Enabled: want false when omitted")
	}
	if ab.SignatureChallenge.Enabled {
		t.Error("SignatureChallenge.Enabled: want false when omitted")
	}
	if ab.SignatureChallenge.RequirePredicate != "none" {
		t.Errorf("SignatureChallenge.RequirePredicate: got %q, want none", ab.SignatureChallenge.RequirePredicate)
	}
}

func TestAbuseDefaults_explicitFalse(t *testing.T) {
	yml := `
abuse:
  pow:
    enabled: false
  api_keys:
    enabled: false
chains:
  - chain_id: osmosis-1
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if cfg.Abuse.PoW.Enabled {
		t.Error("PoW.Enabled: want false when explicitly set to false")
	}
	if cfg.Abuse.APIKeys.Enabled {
		t.Error("APIKeys.Enabled: want false when explicitly set to false")
	}
}

func TestAbuseDefaults_explicitDifficulty(t *testing.T) {
	yml := `
abuse:
  pow:
    difficulty: "hard"
chains:
  - chain_id: osmosis-1
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if cfg.Abuse.PoW.Difficulty != "hard" {
		t.Errorf("PoW.Difficulty: got %q, want hard", cfg.Abuse.PoW.Difficulty)
	}
}

func TestAbuseDefaults_customIntDifficulty(t *testing.T) {
	yml := `
abuse:
  pow:
    difficulty: "75000"
chains:
  - chain_id: osmosis-1
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if cfg.Abuse.PoW.Difficulty != "75000" {
		t.Errorf("PoW.Difficulty: got %q, want 75000", cfg.Abuse.PoW.Difficulty)
	}
}

func TestAbuseValidation_invalidDifficulty(t *testing.T) {
	yml := `
abuse:
  pow:
    difficulty: "extreme"
chains:
  - chain_id: osmosis-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid pow.difficulty")
	}
}

func TestAbuseValidation_invalidPredicate(t *testing.T) {
	yml := `
abuse:
  signature_challenge:
    require_predicate: "mainnet_holder"
chains:
  - chain_id: osmosis-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid signature_challenge.require_predicate")
	}
}

func TestAbuseValidation_predicateNoneValid(t *testing.T) {
	yml := `
abuse:
  signature_challenge:
    require_predicate: "none"
chains:
  - chain_id: osmosis-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("predicate none: unexpected error: %v", err)
	}
}

func TestAbuseValidation_hasBalanceRequiresMinAmount(t *testing.T) {
	yml := `
abuse:
  signature_challenge:
    require_predicate: "has_balance"
chains:
  - chain_id: osmosis-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for has_balance without predicate_min_amount")
	}
}

func TestAbuseValidation_hasBalanceWithMinAmount(t *testing.T) {
	yml := `
abuse:
  signature_challenge:
    require_predicate: "has_balance"
    predicate_min_amount: "1000000uatom"
    predicate_chain_id: "cosmoshub-4"
chains:
  - chain_id: osmosis-1
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sc := cfg.Abuse.SignatureChallenge
	if sc.PredicateMinAmount != "1000000uatom" {
		t.Errorf("PredicateMinAmount: got %q", sc.PredicateMinAmount)
	}
	if sc.PredicateChainID != "cosmoshub-4" {
		t.Errorf("PredicateChainID: got %q", sc.PredicateChainID)
	}
}

func TestAbuseValidation_hasBalanceInvalidMinAmount(t *testing.T) {
	yml := `
abuse:
  signature_challenge:
    require_predicate: "has_balance"
    predicate_min_amount: "notacoin"
chains:
  - chain_id: osmosis-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("expected error for invalid predicate_min_amount")
	}
}

func TestAbuseDefaults_signatureChallenge(t *testing.T) {
	yml := `
abuse:
  signature_challenge:
    enabled: true
chains:
  - chain_id: osmosis-1
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if !cfg.Abuse.SignatureChallenge.Enabled {
		t.Error("SignatureChallenge.Enabled: want true when explicitly set")
	}
	if cfg.Abuse.SignatureChallenge.RequirePredicate != "none" {
		t.Errorf("RequirePredicate: got %q, want none default", cfg.Abuse.SignatureChallenge.RequirePredicate)
	}
}

func TestParseCoin(t *testing.T) {
	tests := []struct {
		input   string
		want    tx.Coin
		wantErr bool
	}{
		{"1000000uosmo", tx.Coin{Amount: "1000000", Denom: "uosmo"}, false},
		{"50000000uosmo", tx.Coin{Amount: "50000000", Denom: "uosmo"}, false},
		{"0uatom", tx.Coin{Amount: "0", Denom: "uatom"}, false},
		{"  100uosmo  ", tx.Coin{Amount: "100", Denom: "uosmo"}, false},
		{"uosmo", tx.Coin{}, true},
		{"1000000", tx.Coin{}, true},
		{"", tx.Coin{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseCoin(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseCoin(%q): err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseCoin(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIBCDefaults_omitted(t *testing.T) {
	cfg, err := LoadChains(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if got := cfg.Chains[0].IBC.Timeout; got != "10m" {
		t.Errorf("IBC.Timeout: got %q, want 10m when omitted", got)
	}
}

func TestIBCDefaults_explicit(t *testing.T) {
	yml := validYAML + `    ibc:
      timeout: "5m"
`
	cfg, err := LoadChains(writeTemp(t, yml))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if got := cfg.Chains[0].IBC.Timeout; got != "5m" {
		t.Errorf("IBC.Timeout: got %q, want 5m", got)
	}
}

func TestIBCDefaults_zeroError(t *testing.T) {
	yml := validYAML + `    ibc:
      timeout: "0"
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("LoadChains: expected error for ibc.timeout 0, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "ibc.timeout") {
		t.Errorf("error %q: want it to mention ibc.timeout", got)
	}
}

const validTwoChainYAML = `
chains:
  - chain_id: simapp-a-1
    enabled: true
    drip:
      anonymous: "1000000stake"
      max_per_address_per_day: "10000000stake"
  - chain_id: simapp-b-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"
      max_per_address_per_day: "10000000uosmo"
    ibc:
      source_chain_id: simapp-a-1
`

func TestIBCSourceChainID_valid(t *testing.T) {
	cfg, err := LoadChains(writeTemp(t, validTwoChainYAML))
	if err != nil {
		t.Fatalf("LoadChains: %v", err)
	}
	if got := cfg.Chains[1].IBC.SourceChainID; got != "simapp-a-1" {
		t.Errorf("IBC.SourceChainID: got %q, want simapp-a-1", got)
	}
}

func TestIBCSourceChainID_unknownSource(t *testing.T) {
	yml := `
chains:
  - chain_id: simapp-a-1
    enabled: true
    drip:
      anonymous: "1000000stake"
      max_per_address_per_day: "10000000stake"
    ibc:
      source_chain_id: nonexistent-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("LoadChains: expected error for unknown source_chain_id, got nil")
	}
	if !strings.Contains(err.Error(), "source_chain_id") {
		t.Errorf("error %q: want it to mention source_chain_id", err.Error())
	}
}

func TestIBCSourceChainID_chainedSources(t *testing.T) {
	yml := `
chains:
  - chain_id: chain-a-1
    enabled: true
    drip:
      anonymous: "1000000stake"
      max_per_address_per_day: "10000000stake"
    ibc:
      source_chain_id: chain-b-1
  - chain_id: chain-b-1
    enabled: true
    drip:
      anonymous: "1000000uosmo"
      max_per_address_per_day: "10000000uosmo"
    ibc:
      source_chain_id: chain-a-1
`
	_, err := LoadChains(writeTemp(t, yml))
	if err == nil {
		t.Fatal("LoadChains: expected error for chained IBC sources, got nil")
	}
	if !strings.Contains(err.Error(), "source_chain_id") {
		t.Errorf("error %q: want it to mention source_chain_id", err.Error())
	}
}
