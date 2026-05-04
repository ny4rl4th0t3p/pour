package chainregistry

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestApplyOverride_enabled(t *testing.T) {
	info := &ChainInfo{Enabled: false}
	src := &FieldSources{}
	enabled := true
	applyOverride(info, src, &ChainOverride{Enabled: &enabled})
	if !info.Enabled {
		t.Error("Enabled: want true after override")
	}
}

func TestApplyOverride_chainName(t *testing.T) {
	info := &ChainInfo{}
	src := &FieldSources{}
	name := "Override Chain"
	applyOverride(info, src, &ChainOverride{ChainName: &name})
	if info.ChainName != "Override Chain" {
		t.Errorf("ChainName: got %q", info.ChainName)
	}
	if src.Identity != SourceConfig {
		t.Errorf("Sources.Identity: got %v, want SourceConfig", src.Identity)
	}
}

func TestApplyOverride_bech32AndSlip44(t *testing.T) {
	info := &ChainInfo{Bech32Prefix: "old"}
	src := &FieldSources{}
	bech32 := "cosmos"
	slip44 := uint32(118)
	applyOverride(info, src, &ChainOverride{Bech32Prefix: &bech32, Slip44: &slip44})
	if info.Bech32Prefix != "cosmos" {
		t.Errorf("Bech32Prefix: got %q", info.Bech32Prefix)
	}
	if info.Slip44 != 118 {
		t.Errorf("Slip44: got %d", info.Slip44)
	}
	if src.Address != SourceConfig {
		t.Errorf("Sources.Address: got %v, want SourceConfig", src.Address)
	}
}

func TestApplyOverride_keyAlgo(t *testing.T) {
	info := &ChainInfo{}
	src := &FieldSources{}
	ka := KeyAlgoEthsecp256k1
	applyOverride(info, src, &ChainOverride{KeyAlgo: &ka})
	if info.KeyAlgo != KeyAlgoEthsecp256k1 {
		t.Errorf("KeyAlgo: got %q", info.KeyAlgo)
	}
}

func TestApplyOverride_networkType(t *testing.T) {
	info := &ChainInfo{}
	src := &FieldSources{}
	nt := NetworkTypeTestnet
	applyOverride(info, src, &ChainOverride{NetworkType: &nt})
	if info.NetworkType != NetworkTypeTestnet {
		t.Errorf("NetworkType: got %q", info.NetworkType)
	}
}

func TestApplyEndpoints_grpcOnly(t *testing.T) {
	info := &ChainInfo{}
	src := &FieldSources{}
	applyEndpoints(info, src, &EndpointsOverride{GRPC: []string{"grpc.example.com:9090"}})
	if len(info.Endpoints.GRPC) != 1 || info.Endpoints.GRPC[0].URL != "grpc.example.com:9090" {
		t.Errorf("GRPC: got %v", info.Endpoints.GRPC)
	}
	if src.Endpoints != SourceConfig {
		t.Errorf("Sources.Endpoints: want SourceConfig")
	}
}

func TestApplyEndpoints_nil(t *testing.T) {
	info := &ChainInfo{Endpoints: Endpoints{GRPC: []Endpoint{{URL: "grpc.example.com:9090"}}}}
	src := &FieldSources{}
	applyEndpoints(info, src, nil)
	if len(info.Endpoints.GRPC) != 1 {
		t.Error("nil override should leave endpoints unchanged")
	}
}

func TestApplyEndpoints_rpcAndRest(t *testing.T) {
	info := &ChainInfo{}
	src := &FieldSources{}
	applyEndpoints(info, src, &EndpointsOverride{
		RPC:  []string{"https://rpc.example.com"},
		REST: []string{"https://rest.example.com"},
	})
	if len(info.Endpoints.RPC) != 1 {
		t.Errorf("RPC: got %v", info.Endpoints.RPC)
	}
	if len(info.Endpoints.REST) != 1 {
		t.Errorf("REST: got %v", info.Endpoints.REST)
	}
}

func TestApplyFeeTokens_matchingDenom(t *testing.T) {
	low := "0.001"
	avg := "0.025"
	info := &ChainInfo{FeeTokens: []FeeToken{{Denom: "uosmo"}}}
	src := &FieldSources{}
	applyFeeTokens(info, src, []FeeTokenOverride{
		{Denom: "uosmo", LowGasPrice: &low, AverageGasPrice: &avg},
	})
	if info.FeeTokens[0].LowGasPrice.String() != "0.001" {
		t.Errorf("LowGasPrice: got %s", info.FeeTokens[0].LowGasPrice.String())
	}
	if info.FeeTokens[0].AverageGasPrice.String() != "0.025" {
		t.Errorf("AverageGasPrice: got %s", info.FeeTokens[0].AverageGasPrice.String())
	}
}

func TestApplyFeeTokens_noMatchingDenom(t *testing.T) {
	low := "0.001"
	info := &ChainInfo{FeeTokens: []FeeToken{{Denom: "uatom"}}}
	src := &FieldSources{}
	applyFeeTokens(info, src, []FeeTokenOverride{{Denom: "uosmo", LowGasPrice: &low}})
	if !info.FeeTokens[0].LowGasPrice.IsZero() {
		t.Error("non-matching denom should not change fee token")
	}
}

func TestApplyDecimalField_valid(t *testing.T) {
	var d decimal.Decimal
	var src Source
	raw := "0.025"
	applyDecimalField(&d, &src, &raw)
	if d.String() != "0.025" {
		t.Errorf("got %s, want 0.025", d.String())
	}
	if src != SourceConfig {
		t.Errorf("src: got %v, want SourceConfig", src)
	}
}

func TestApplyDecimalField_nil(t *testing.T) {
	d := decimal.NewFromFloat(1.0)
	var src Source
	applyDecimalField(&d, &src, nil)
	if d.String() != "1" {
		t.Error("nil pointer should not change field")
	}
}

func TestSetOverrides_updatesChain(t *testing.T) {
	s, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	slip44 := uint32(118)
	s.AddStandalone(ChainInfo{
		ChainID:      "mynet-1",
		Bech32Prefix: "mynet",
		Slip44:       slip44,
		Enabled:      true,
	})

	newName := "Override Name"
	bech32 := "mynet"
	ov := &OverrideSet{Chains: map[string]*ChainOverride{
		"mynet-1": {Bech32Prefix: &bech32, ChainName: &newName},
	}}
	s.SetOverrides(ov)

	info, err := s.Get("mynet-1")
	if err != nil {
		t.Fatal("chain not found after SetOverrides")
	}
	if info.ChainName != "Override Name" {
		t.Errorf("ChainName: got %q, want Override Name", info.ChainName)
	}
}
