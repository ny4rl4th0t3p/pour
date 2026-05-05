package tx

import (
	"context"
	"testing"

	"google.golang.org/protobuf/types/known/anypb"

	"github.com/shopspring/decimal"

	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

func txSvcClient(t *testing.T, cfg fakechain.Config) txv1beta1.ServiceClient {
	t.Helper()
	return txv1beta1.NewServiceClient(fakechain.Start(t, cfg))
}

func oneMsgSend(t *testing.T) []*anypb.Any {
	t.Helper()
	msg, err := buildMsgSend(testFromAddr, testToAddr, Coins{{Denom: "uosmo", Amount: "1000000"}})
	if err != nil {
		t.Fatalf("buildMsgSend: %v", err)
	}
	return []*anypb.Any{msg}
}

// TestEstimateFee_trustedCache verifies the trusted cache path (SampleCount ≥ 5).
func TestEstimateFee_trustedCache(t *testing.T) {
	svc := txSvcClient(t, fakechain.Config{})
	msgs := oneMsgSend(t)

	cached := &CachedEstimate{
		BaseGas:        180_000,
		GasPerOutput:   70_000,
		FeeDenom:       "uosmo",
		GasPriceAmount: "0.025",
		SampleCount:    5, // trusted
	}
	opts := feeOpts{GasCache: &stubCache{estimate: cached}, OutputCount: 1, MsgType: MsgTypeSend}
	chain := &chainregistry.ChainInfo{ChainID: "osmosis-1"}

	est, err := estimateFee(t.Context(), svc, chain, msgs, opts, nil)
	if err != nil {
		t.Fatalf("estimateFee: %v", err)
	}
	// (180_000 + 70_000) * 1.3 = 325_000
	if est.GasLimit != 325_000 {
		t.Errorf("GasLimit: got %d, want 325000", est.GasLimit)
	}
	if est.Fee.Denom != "uosmo" {
		t.Errorf("Fee.Denom: got %s, want uosmo", est.Fee.Denom)
	}
}

// TestEstimateFee_simulate verifies the simulation path.
func TestEstimateFee_simulate(t *testing.T) {
	svc := txSvcClient(t, fakechain.Config{GasUsed: 100_000})
	msgs := oneMsgSend(t)
	chain := &chainregistry.ChainInfo{
		ChainID:   "osmosis-1",
		FeeTokens: []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	est, err := estimateFee(t.Context(), svc, chain, msgs, feeOpts{OutputCount: 1, MsgType: MsgTypeSend}, nil)
	if err != nil {
		t.Fatalf("estimateFee: %v", err)
	}
	// 100_000 * 1.5 = 150_000
	if est.GasLimit != 150_000 {
		t.Errorf("GasLimit: got %d, want 150000", est.GasLimit)
	}
}

// TestEstimateFee_registryFallback verifies fallback to chain registry gas price
// when simulation is not available.
func TestEstimateFee_registryFallback(t *testing.T) {
	svc := txSvcClient(t, fakechain.Config{GasUsed: 0}) // simulate returns Unimplemented
	msgs := oneMsgSend(t)
	chain := &chainregistry.ChainInfo{
		ChainID:   "osmosis-1",
		FeeTokens: []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	est, err := estimateFee(t.Context(), svc, chain, msgs, feeOpts{OutputCount: 1, MsgType: MsgTypeSend}, nil)
	if err != nil {
		t.Fatalf("estimateFee: %v", err)
	}
	// (200_000 + 80_000) * 1.5 = 420_000
	if est.GasLimit != 420_000 {
		t.Errorf("GasLimit: got %d, want 420000", est.GasLimit)
	}
	if est.Fee.Denom != "uosmo" {
		t.Errorf("Fee.Denom: got %s, want uosmo", est.Fee.Denom)
	}
}

// TestEstimateFee_staticDefaults verifies the hard-coded static fallback.
func TestEstimateFee_staticDefaults(t *testing.T) {
	svc := txSvcClient(t, fakechain.Config{GasUsed: 0})
	msgs := oneMsgSend(t)
	chain := &chainregistry.ChainInfo{ChainID: "osmosis-1"} // no FeeTokens

	est, err := estimateFee(t.Context(), svc, chain, msgs, feeOpts{OutputCount: 1, MsgType: MsgTypeSend}, nil)
	if err != nil {
		t.Fatalf("estimateFee: %v", err)
	}
	// (200_000 + 80_000) * 1.5 = 420_000
	if est.GasLimit != 420_000 {
		t.Errorf("GasLimit: got %d, want 420000", est.GasLimit)
	}
	if est.Fee.Denom != defaultFeeDenom {
		t.Errorf("Fee.Denom: got %s, want %s", est.Fee.Denom, defaultFeeDenom)
	}
}

// stubCache is a minimal GasCache implementation for fee estimation tests.
type stubCache struct {
	estimate *CachedEstimate
}

func (s *stubCache) Lookup(_ context.Context, _, _ string) (*CachedEstimate, bool) {
	if s.estimate == nil {
		return nil, false
	}
	return s.estimate, true
}

func (*stubCache) RecordSuccess(_ context.Context, _, _ string, _ uint64, _ int, _, _ string) error {
	return nil
}

func (*stubCache) RecordFailure(_ context.Context, _, _, _ string) error { return nil }
