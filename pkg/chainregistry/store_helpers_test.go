package chainregistry

import (
	"testing"
)

func TestApplyAcceptedField_chainID(t *testing.T) {
	info := &ChainInfo{ChainID: "old-1"}
	if err := applyAcceptedField(info, FieldChainID, "new-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ChainID != "new-1" {
		t.Errorf("ChainID: got %q, want new-1", info.ChainID)
	}
}

func TestApplyAcceptedField_chainName(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, FieldChainName, "My Chain"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ChainName != "My Chain" {
		t.Errorf("ChainName: got %q", info.ChainName)
	}
}

func TestApplyAcceptedField_networkType_typed(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, FieldNetworkType, NetworkTypeTestnet); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.NetworkType != NetworkTypeTestnet {
		t.Errorf("NetworkType: got %q", info.NetworkType)
	}
}

func TestApplyAcceptedField_networkType_string(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, FieldNetworkType, "mainnet"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.NetworkType != NetworkTypeMainnet {
		t.Errorf("NetworkType: got %q", info.NetworkType)
	}
}

func TestApplyAcceptedField_bech32Prefix(t *testing.T) {
	info := &ChainInfo{Bech32Prefix: "old"}
	if err := applyAcceptedField(info, FieldBech32Prefix, "cosmos"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Bech32Prefix != "cosmos" {
		t.Errorf("Bech32Prefix: got %q", info.Bech32Prefix)
	}
}

func TestApplyAcceptedField_slip44(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, FieldSlip44, uint32(118)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Slip44 != 118 {
		t.Errorf("Slip44: got %d", info.Slip44)
	}
}

func TestApplyAcceptedField_keyAlgo_typed(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, FieldKeyAlgo, KeyAlgoSecp256k1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.KeyAlgo != KeyAlgoSecp256k1 {
		t.Errorf("KeyAlgo: got %q", info.KeyAlgo)
	}
}

func TestApplyAcceptedField_keyAlgo_string(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, FieldKeyAlgo, "ethsecp256k1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.KeyAlgo != KeyAlgoEthsecp256k1 {
		t.Errorf("KeyAlgo: got %q", info.KeyAlgo)
	}
}

func TestApplyAcceptedField_feeTokensDenom(t *testing.T) {
	info := &ChainInfo{}
	tokens := []FeeToken{{Denom: "uosmo"}, {Denom: "uatom"}}
	if err := applyAcceptedField(info, FieldFeeTokensDenom, tokens); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.FeeTokens) != 2 || info.FeeTokens[0].Denom != "uosmo" {
		t.Errorf("FeeTokens: got %v", info.FeeTokens)
	}
}

func TestApplyAcceptedField_unknown(t *testing.T) {
	info := &ChainInfo{}
	if err := applyAcceptedField(info, "unknown_field", "value"); err == nil {
		t.Fatal("expected error for unknown field")
	}
}
