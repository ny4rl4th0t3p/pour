package devnet

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGenesis writes genesis JSON to <dir>/config/genesis.json.
func writeGenesis(t *testing.T, dir, content string) {
	t.Helper()
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "genesis.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write genesis.json: %v", err)
	}
}

const standardGenesis = `{
  "chain_id": "mychain-1",
  "app_state": {
    "staking": {
      "params": {
        "bond_denom": "uatom"
      }
    },
    "bank": {
      "balances": [
        {
          "address": "cosmos1qnk2n4nlkpw9xfqntladh74er2xa62wgas3yk",
          "coins": [
            {"denom": "uatom", "amount": "10000000000"}
          ]
        },
        {
          "address": "cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
          "coins": [
            {"denom": "uatom", "amount": "5000000000"},
            {"denom": "stake",  "amount": "1000000"}
          ]
        }
      ]
    }
  }
}`

func TestParseGenesis_standard(t *testing.T) {
	dir := t.TempDir()
	writeGenesis(t, dir, standardGenesis)

	info, err := ParseGenesis(dir)
	if err != nil {
		t.Fatalf("ParseGenesis: %v", err)
	}

	if info.ChainID != "mychain-1" {
		t.Errorf("ChainID: got %q, want %q", info.ChainID, "mychain-1")
	}
	if info.NativeDenom != "uatom" {
		t.Errorf("NativeDenom: got %q, want %q", info.NativeDenom, "uatom")
	}
	if info.Bech32Prefix != "cosmos" {
		t.Errorf("Bech32Prefix: got %q, want %q", info.Bech32Prefix, "cosmos")
	}
	if len(info.Balances) != 2 {
		t.Errorf("Balances len: got %d, want 2", len(info.Balances))
	}

	first := info.Balances["cosmos1qnk2n4nlkpw9xfqntladh74er2xa62wgas3yk"]
	if len(first) != 1 || first[0].Denom != "uatom" || first[0].Amount != "10000000000" {
		t.Errorf("first balance: got %v", first)
	}
	second := info.Balances["cosmos1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh"]
	if len(second) != 2 {
		t.Errorf("second balance coins: got %d, want 2", len(second))
	}
}

func TestParseGenesis_nonCosmosPrefix(t *testing.T) {
	dir := t.TempDir()
	writeGenesis(t, dir, `{
	  "chain_id": "osmosis-1",
	  "app_state": {
	    "staking": {"params": {"bond_denom": "uosmo"}},
	    "bank": {
	      "balances": [
	        {"address": "osmo1qnk2n4nlkpw9xfqntladh74er2xa62wgahuxegs", "coins": [{"denom": "uosmo", "amount": "1000"}]}
	      ]
	    }
	  }
	}`)

	info, err := ParseGenesis(dir)
	if err != nil {
		t.Fatalf("ParseGenesis: %v", err)
	}
	if info.Bech32Prefix != "osmo" {
		t.Errorf("Bech32Prefix: got %q, want %q", info.Bech32Prefix, "osmo")
	}
	if info.NativeDenom != "uosmo" {
		t.Errorf("NativeDenom: got %q, want %q", info.NativeDenom, "uosmo")
	}
}

func TestParseGenesis_noBalances_defaultsToCosmosPrefix(t *testing.T) {
	dir := t.TempDir()
	writeGenesis(t, dir, `{
	  "chain_id": "devnet-1",
	  "app_state": {
	    "staking": {"params": {"bond_denom": "ustake"}},
	    "bank": {"balances": []}
	  }
	}`)

	info, err := ParseGenesis(dir)
	if err != nil {
		t.Fatalf("ParseGenesis: %v", err)
	}
	if info.Bech32Prefix != "cosmos" {
		t.Errorf("Bech32Prefix: got %q, want %q (default)", info.Bech32Prefix, "cosmos")
	}
}

func TestParseGenesis_missingChainID(t *testing.T) {
	dir := t.TempDir()
	writeGenesis(t, dir, `{"app_state": {"staking": {"params": {}},"bank": {"balances": []}}}`)

	_, err := ParseGenesis(dir)
	if err == nil {
		t.Fatal("expected error for missing chain_id, got nil")
	}
}

func TestParseGenesis_fileNotFound(t *testing.T) {
	_, err := ParseGenesis(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing genesis.json, got nil")
	}
}

func TestParseGenesis_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeGenesis(t, dir, `{not valid json`)

	_, err := ParseGenesis(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExtractBech32Prefix(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"cosmos1qnk2n4nlkpw9xfqntladh74er2xa62wgas3yk", "cosmos"},
		{"osmo1qnk2n4nlkpw9xfqntladh74er2xa62wgahuxegs", "osmo"},
		{"neutron1abcdefghjkl", "neutron"},
		{"", ""},
		{"noone", ""},
	}
	for _, tc := range tests {
		got := extractBech32Prefix(tc.addr)
		if got != tc.want {
			t.Errorf("extractBech32Prefix(%q): got %q, want %q", tc.addr, got, tc.want)
		}
	}
}
