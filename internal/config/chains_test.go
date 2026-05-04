package config

import (
	"os"
	"path/filepath"
	"testing"

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
	if co.Drip.Anonymous != "1000000uosmo" {
		t.Errorf("Drip.Anonymous: got %s", co.Drip.Anonymous)
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
