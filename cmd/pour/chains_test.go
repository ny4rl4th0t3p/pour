package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

func TestChainsValidateCmd(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "valid registry chain",
			config:  "testdata/valid-registry.yml",
			wantErr: false,
		},
		{
			name:    "valid standalone chain",
			config:  "testdata/valid-standalone.yml",
			wantErr: false,
		},
		{
			name:    "source-only registry chain (no drip, no ibc.drips)",
			config:  "testdata/invalid-missing-drip.yml",
			wantErr: false,
		},
		{
			name:    "standalone missing bech32_prefix",
			config:  "testdata/invalid-standalone-no-bech32.yml",
			wantErr: true,
		},
		{
			name:    "file not found",
			config:  "testdata/does-not-exist.yml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &ChainsValidateCmd{Config: tt.config}
			err := cmd.Run()
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ----- stringSliceEqual -----

func TestStringSliceEqual_equal(t *testing.T) {
	if !stringSliceEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("equal slices reported as unequal")
	}
}

func TestStringSliceEqual_differentLengths(t *testing.T) {
	if stringSliceEqual([]string{"a"}, []string{"a", "b"}) {
		t.Error("different-length slices reported as equal")
	}
}

func TestStringSliceEqual_differentElements(t *testing.T) {
	if stringSliceEqual([]string{"a", "b"}, []string{"a", "c"}) {
		t.Error("slices with different elements reported as equal")
	}
}

func TestStringSliceEqual_bothNil(t *testing.T) {
	if !stringSliceEqual(nil, nil) {
		t.Error("two nil slices should be equal")
	}
}

// ----- endpointURLs -----

func TestEndpointURLs(t *testing.T) {
	eps := []chainEndpoint{{URL: "grpc.example.com:9090"}, {URL: "grpc2.example.com:9090"}}
	urls := endpointURLs(eps)
	if len(urls) != 2 || urls[0] != "grpc.example.com:9090" || urls[1] != "grpc2.example.com:9090" {
		t.Errorf("endpointURLs: got %v", urls)
	}
}

func TestEndpointURLs_empty(t *testing.T) {
	if urls := endpointURLs(nil); len(urls) != 0 {
		t.Errorf("empty input: got %v", urls)
	}
}

// ----- snapFeeToken -----

func TestSnapFeeToken_found(t *testing.T) {
	fts := []chainFeeToken{{Denom: "uosmo", LowGasPrice: "0.001"}, {Denom: "uatom"}}
	ft := snapFeeToken(fts, "uosmo")
	if ft == nil || ft.LowGasPrice != "0.001" {
		t.Errorf("snapFeeToken: got %v", ft)
	}
}

func TestSnapFeeToken_notFound(t *testing.T) {
	fts := []chainFeeToken{{Denom: "uatom"}}
	if snapFeeToken(fts, "uosmo") != nil {
		t.Error("expected nil for missing denom")
	}
}

// ----- formatAny -----

func TestFormatAny_nil(t *testing.T) {
	if got := formatAny(nil); got != "<nil>" {
		t.Errorf("nil: got %q, want <nil>", got)
	}
}

func TestFormatAny_string(t *testing.T) {
	if got := formatAny("hello"); got != "hello" {
		t.Errorf("string: got %q, want hello", got)
	}
}

func TestFormatAny_number(t *testing.T) {
	if got := formatAny(42); got != "42" {
		t.Errorf("number: got %q, want 42", got)
	}
}

func TestFormatAny_slice(t *testing.T) {
	got := formatAny([]string{"a", "b"})
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("slice: got %q", got)
	}
}

// ----- endpointsSnippet -----

func TestEndpointsSnippet(t *testing.T) {
	eps := []chainEndpoint{{URL: "grpc.example.com:9090"}}
	got := endpointsSnippet("grpc", eps)
	want := "endpoints:\n  grpc:\n    - grpc.example.com:9090\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEndpointsSnippet_empty(t *testing.T) {
	got := endpointsSnippet("rpc", nil)
	if !strings.HasPrefix(got, "endpoints:\n  rpc:\n") {
		t.Errorf("empty endpoints: got %q", got)
	}
}

// ----- feeTokenPriceSnippet -----

func TestFeeTokenPriceSnippet_empty(t *testing.T) {
	if got := feeTokenPriceSnippet(nil, "low_gas_price"); got != "fee_tokens: []\n" {
		t.Errorf("empty: got %q", got)
	}
}

func TestFeeTokenPriceSnippet_lowGasPrice(t *testing.T) {
	fts := []chainFeeToken{{Denom: "uosmo", LowGasPrice: "0.001"}}
	got := feeTokenPriceSnippet(fts, "low_gas_price")
	if !strings.Contains(got, "uosmo") || !strings.Contains(got, "0.001") {
		t.Errorf("low_gas_price: got %q", got)
	}
}

func TestFeeTokenPriceSnippet_averageGasPrice(t *testing.T) {
	fts := []chainFeeToken{{Denom: "uosmo", AverageGasPrice: "0.025"}}
	got := feeTokenPriceSnippet(fts, "average_gas_price")
	if !strings.Contains(got, "0.025") {
		t.Errorf("average_gas_price: got %q", got)
	}
}

func TestFeeTokenPriceSnippet_highGasPrice(t *testing.T) {
	fts := []chainFeeToken{{Denom: "uosmo", HighGasPrice: "0.05"}}
	got := feeTokenPriceSnippet(fts, "high_gas_price")
	if !strings.Contains(got, "0.05") {
		t.Errorf("high_gas_price: got %q", got)
	}
}

// ----- pinSnippet -----

func testSnap() *chainSnapshot {
	return &chainSnapshot{
		ChainName:    "osmosis",
		NetworkType:  "mainnet",
		KeyAlgo:      "secp256k1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		BlockTime:    int64(6 * time.Second),
		Endpoints: chainEndpoints{
			GRPC: []chainEndpoint{{URL: "grpc.osmosis.zone:9090"}},
			RPC:  []chainEndpoint{{URL: "https://rpc.osmosis.zone"}},
			REST: []chainEndpoint{{URL: "https://rest.osmosis.zone"}},
		},
		FeeTokens: []chainFeeToken{
			{Denom: "uosmo", LowGasPrice: "0.001", AverageGasPrice: "0.025", HighGasPrice: "0.04"},
		},
	}
}

func TestPinSnippet_knownFields(t *testing.T) {
	tests := []struct {
		field string
		want  string
	}{
		{"chain_name", "osmosis"},
		{"chainname", "osmosis"},
		{"network_type", "mainnet"},
		{"networktype", "mainnet"},
		{"key_algo", "secp256k1"},
		{"keyalgo", "secp256k1"},
		{"bech32_prefix", "osmo"},
		{"bech32prefix", "osmo"},
		{"slip44", "118"},
		{"block_time", "6s"},
		{"blocktime", "6s"},
		{"endpoints.grpc", "grpc.osmosis.zone:9090"},
		{"endpoints.rpc", "rpc.osmosis.zone"},
		{"endpoints.rest", "rest.osmosis.zone"},
		{"fee_tokens.low_gas_price", "0.001"},
		{"feetokens.lowgasprice", "0.001"},
		{"fee_tokens.average_gas_price", "0.025"},
		{"feetokens.averagegasprice", "0.025"},
		{"fee_tokens.high_gas_price", "0.04"},
		{"feetokens.highgasprice", "0.04"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got, err := pinSnippet(testSnap(), tt.field)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("field %q: got %q, want it to contain %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestPinSnippet_unknownField(t *testing.T) {
	if _, err := pinSnippet(testSnap(), "unknown_field"); err == nil {
		t.Error("expected error for unknown field")
	}
}

// ----- diff helpers -----

func strPtrC(s string) *string    { return &s }
func uint32PtrC(v uint32) *uint32 { return &v }

func TestDiffIdentityLines_noChanges(t *testing.T) {
	cc := &config.ChainConfig{}
	snap := &chainSnapshot{ChainName: "osmosis"}
	if lines := diffIdentityLines(cc, snap); len(lines) != 0 {
		t.Errorf("no overrides: expected no lines, got %v", lines)
	}
}

func TestDiffIdentityLines_allFieldsChanged(t *testing.T) {
	bt := "6s"
	cc := &config.ChainConfig{
		ChainName:    strPtrC("new-name"),
		NetworkType:  strPtrC("testnet"),
		KeyAlgo:      strPtrC("ethsecp256k1"),
		Bech32Prefix: strPtrC("new"),
		Slip44:       uint32PtrC(60),
		BlockTime:    &bt,
	}
	snap := &chainSnapshot{
		ChainName:    "old-name",
		NetworkType:  "mainnet",
		KeyAlgo:      "secp256k1",
		Bech32Prefix: "old",
		Slip44:       118,
		BlockTime:    int64(3 * time.Second),
	}
	lines := diffIdentityLines(cc, snap)
	if len(lines) != 6 {
		t.Errorf("all fields changed: expected 6 lines, got %d: %v", len(lines), lines)
	}
}

func TestDiffEndpointLines_nilEndpoints(t *testing.T) {
	cc := &config.ChainConfig{}
	snap := &chainSnapshot{}
	if lines := diffEndpointLines(cc, snap); lines != nil {
		t.Errorf("nil endpoints: expected nil, got %v", lines)
	}
}

func TestDiffEndpointLines_allProtocolsChanged(t *testing.T) {
	cc := &config.ChainConfig{
		Endpoints: &config.EndpointsConfig{
			GRPC: []string{"new.grpc.com:9090"},
			RPC:  []string{"new.rpc.com"},
			REST: []string{"new.rest.com"},
		},
	}
	snap := &chainSnapshot{Endpoints: chainEndpoints{
		GRPC: []chainEndpoint{{URL: "old.grpc.com:9090"}},
		RPC:  []chainEndpoint{{URL: "old.rpc.com"}},
		REST: []chainEndpoint{{URL: "old.rest.com"}},
	}}
	lines := diffEndpointLines(cc, snap)
	if len(lines) != 3 {
		t.Errorf("all protocols changed: expected 3 lines, got %d: %v", len(lines), lines)
	}
}

func TestDiffEndpointLines_grpcSame(t *testing.T) {
	cc := &config.ChainConfig{
		Endpoints: &config.EndpointsConfig{GRPC: []string{"grpc.com:9090"}},
	}
	snap := &chainSnapshot{Endpoints: chainEndpoints{GRPC: []chainEndpoint{{URL: "grpc.com:9090"}}}}
	if lines := diffEndpointLines(cc, snap); len(lines) != 0 {
		t.Errorf("grpc same: expected no lines, got %v", lines)
	}
}

func TestDiffFeeTokenLines_noMatch(t *testing.T) {
	low := "0.001"
	cc := &config.ChainConfig{FeeTokens: []config.FeeTokenConfig{{Denom: "uosmo", LowGasPrice: &low}}}
	snap := &chainSnapshot{FeeTokens: []chainFeeToken{{Denom: "uatom"}}}
	if lines := diffFeeTokenLines(cc, snap); len(lines) != 0 {
		t.Errorf("no denom match: expected no lines, got %v", lines)
	}
}

func TestDiffFeeTokenLines_allPricesChanged(t *testing.T) {
	low := "0.002"
	avg := "0.05"
	high := "0.1"
	cc := &config.ChainConfig{FeeTokens: []config.FeeTokenConfig{
		{Denom: "uosmo", LowGasPrice: &low, AverageGasPrice: &avg, HighGasPrice: &high},
	}}
	snap := &chainSnapshot{FeeTokens: []chainFeeToken{
		{Denom: "uosmo", LowGasPrice: "0.001", AverageGasPrice: "0.025", HighGasPrice: "0.04"},
	}}
	lines := diffFeeTokenLines(cc, snap)
	if len(lines) != 3 {
		t.Errorf("all prices changed: expected 3 lines, got %d: %v", len(lines), lines)
	}
}

func TestDiffChainLines_enabledChanged(t *testing.T) {
	enabled := true
	cc := &config.ChainConfig{Enabled: &enabled}
	snap := &chainSnapshot{Enabled: false}
	lines := diffChainLines(cc, snap)
	if len(lines) == 0 || !strings.Contains(lines[0], "enabled") {
		t.Errorf("enabled change: got %v", lines)
	}
}

func TestDiffChainLines_noChanges(t *testing.T) {
	cc := &config.ChainConfig{}
	snap := &chainSnapshot{}
	if lines := diffChainLines(cc, snap); len(lines) != 0 {
		t.Errorf("no changes: got %v", lines)
	}
}
