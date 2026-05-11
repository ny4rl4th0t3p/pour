package devnet

import (
	"testing"
)

var baseInfo = &GenesisInfo{
	ChainID:      "mychain-1",
	Bech32Prefix: "cosmos",
	NativeDenom:  "uatom",
	Balances:     map[string][]Coin{},
}

func TestBuildConfig_standard(t *testing.T) {
	cfg, err := BuildConfig(baseInfo, "localhost:9090", "", "500000uatom")
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	if len(cfg.Chains) != 1 {
		t.Fatalf("chains: got %d, want 1", len(cfg.Chains))
	}
	ch := cfg.Chains[0]
	if ch.ChainID != "mychain-1" {
		t.Errorf("ChainID: got %q", ch.ChainID)
	}
	if !ch.Standalone {
		t.Error("Standalone: want true")
	}
	if ch.Bech32Prefix == nil || *ch.Bech32Prefix != "cosmos" {
		t.Errorf("Bech32Prefix: got %v", ch.Bech32Prefix)
	}
	if ch.Slip44 == nil || *ch.Slip44 != 118 {
		t.Errorf("Slip44: got %v", ch.Slip44)
	}
	if ch.Endpoints == nil || len(ch.Endpoints.GRPC) != 1 || ch.Endpoints.GRPC[0] != "localhost:9090" {
		t.Errorf("Endpoints.GRPC: got %v", ch.Endpoints)
	}
	if ch.Drip.Anonymous != "500000uatom" {
		t.Errorf("Drip.Anonymous: got %q", ch.Drip.Anonymous)
	}
	if ch.Drip.MaxPerAddressPerDay != "10000000uatom" {
		t.Errorf("Drip.MaxPerAddressPerDay: got %q", ch.Drip.MaxPerAddressPerDay)
	}
	if ch.BatchWindow != "0s" {
		t.Errorf("BatchWindow: got %q, want 0s", ch.BatchWindow)
	}
	if ch.IBC.Timeout != "10m" {
		t.Errorf("IBC.Timeout: got %q, want 10m", ch.IBC.Timeout)
	}
}

func TestBuildConfig_defaultDrip(t *testing.T) {
	cfg, err := BuildConfig(baseInfo, "localhost:9090", "", "")
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	ch := cfg.Chains[0]
	if ch.Drip.Anonymous != "1000000uatom" {
		t.Errorf("default drip: got %q, want 1000000uatom", ch.Drip.Anonymous)
	}
	if ch.Drip.MaxPerAddressPerDay != "10000000uatom" {
		t.Errorf("default max drip: got %q", ch.Drip.MaxPerAddressPerDay)
	}
}

func TestBuildConfig_emptyDenom(t *testing.T) {
	info := &GenesisInfo{ChainID: "x-1", Bech32Prefix: "cosmos", NativeDenom: ""}
	_, err := BuildConfig(info, "localhost:9090", "", "")
	if err == nil {
		t.Fatal("expected error for empty NativeDenom, got nil")
	}
}

func TestBuildConfig_RESTOnly(t *testing.T) {
	cfg, err := BuildConfig(baseInfo, "", "http://localhost:1317", "")
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	ch := cfg.Chains[0]
	if ch.Endpoints == nil {
		t.Fatal("Endpoints: nil")
	}
	if len(ch.Endpoints.GRPC) != 0 {
		t.Errorf("Endpoints.GRPC: want empty, got %v", ch.Endpoints.GRPC)
	}
	if len(ch.Endpoints.REST) != 1 || ch.Endpoints.REST[0] != "http://localhost:1317" {
		t.Errorf("Endpoints.REST: got %v", ch.Endpoints.REST)
	}
}

func TestBuildConfig_bothEndpoints(t *testing.T) {
	cfg, err := BuildConfig(baseInfo, "localhost:9090", "http://localhost:1317", "")
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	ch := cfg.Chains[0]
	if len(ch.Endpoints.GRPC) != 1 {
		t.Errorf("Endpoints.GRPC: got %v", ch.Endpoints.GRPC)
	}
	if len(ch.Endpoints.REST) != 1 {
		t.Errorf("Endpoints.REST: got %v", ch.Endpoints.REST)
	}
}
