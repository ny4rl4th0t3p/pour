package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
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
	if !c.Enabled {
		t.Error("Enabled: want true")
	}
	if c.Bech32Prefix != "osmo" {
		t.Errorf("Bech32Prefix: got %s, want osmo", c.Bech32Prefix)
	}
	if c.Slip44 != 118 {
		t.Errorf("Slip44: got %d, want 118", c.Slip44)
	}
	if len(c.Endpoints.GRPC) != 1 || c.Endpoints.GRPC[0] != "grpc.osmosis.zone:9090" {
		t.Errorf("Endpoints.GRPC: got %v", c.Endpoints.GRPC)
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
