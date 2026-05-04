package main

import (
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

func TestLowGasPriceFn_chainWithLowGasPrice(t *testing.T) {
	low := "0.001"
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{
			ChainID:   "osmosis-1",
			FeeTokens: []config.FeeTokenConfig{{Denom: "uosmo", LowGasPrice: &low}},
		}},
	}
	fn, err := lowGasPriceFn(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := fn("osmosis-1")
	if !ok || v != "0.001" {
		t.Errorf("osmosis-1: got (%q, %v), want (0.001, true)", v, ok)
	}
}

func TestLowGasPriceFn_unknownChain(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{ChainID: "osmosis-1"}},
	}
	fn, err := lowGasPriceFn(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := fn("unknown-1")
	if ok || v != "" {
		t.Errorf("unknown chain: got (%q, %v), want ('', false)", v, ok)
	}
}

func TestLowGasPriceFn_chainWithNoFeeTokens(t *testing.T) {
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{ChainID: "osmosis-1"}},
	}
	fn, err := lowGasPriceFn(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := fn("osmosis-1")
	if ok || v != "" {
		t.Errorf("no fee tokens: got (%q, %v), want ('', false)", v, ok)
	}
}

func TestLowGasPriceFn_invalidBlockTime(t *testing.T) {
	bt := "not-a-duration"
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{{ChainID: "mynet-1", BlockTime: &bt}},
	}
	if _, err := lowGasPriceFn(cfg); err == nil {
		t.Fatal("expected error for invalid block_time")
	}
}
