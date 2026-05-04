package tx

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shopspring/decimal"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

const (
	defaultBaseGas      uint64 = 200_000
	defaultGasPerOutput uint64 = 80_000
	defaultFeeDenom            = "uosmo"
)

var (
	defaultGasPrice  = decimal.NewFromFloat(0.025)
	gasAdjustTrusted = decimal.NewFromFloat(1.3)
	gasAdjustCold    = decimal.NewFromFloat(1.5)
)

// estimateFee determines the gas limit and fee for a set of messages.
// Fallback hierarchy (v0.1.0, no operator override):
//  1. Trusted cache entry (SampleCount ≥ 5) → 1.3× adjustment
//  2. Simulate → 1.5× cold-start adjustment
//  3. Chain registry average gas price + static gas
//  4. Hard-coded static defaults
func estimateFee(
	ctx context.Context,
	txSvc txv1beta1.ServiceClient,
	chain *chainregistry.ChainInfo,
	msgs []*anypb.Any,
	req SendRequest,
	logger *slog.Logger,
) (Estimate, error) {
	// 1. Trusted cache.
	if req.GasCache != nil {
		if cached, ok := req.GasCache.Lookup(ctx, chain.ChainID); ok && cached.IsTrusted() {
			baseGas := decimal.NewFromInt(int64(cached.BaseGas + cached.GasPerOutput*uint64(len(msgs))))
			gas := baseGas.Mul(gasAdjustTrusted).Ceil().BigInt().Uint64()
			fee, err := calcFee(gas, cached.GasPriceAmount, cached.FeeDenom)
			if err == nil {
				return Estimate{GasLimit: gas, Fee: fee}, nil
			}
		}
	}

	// 2. Simulate.
	if estimate, ok, err := trySimulate(ctx, txSvc, chain, msgs); ok {
		return estimate, err
	}

	// 3. Chain registry average gas price.
	if len(chain.FeeTokens) > 0 {
		ft := chain.FeeTokens[0]
		baseGas := decimal.NewFromInt(int64(defaultBaseGas + defaultGasPerOutput*uint64(len(msgs))))
		gas := baseGas.Mul(gasAdjustCold).Ceil().BigInt().Uint64()
		fee, err := calcFee(gas, ft.AverageGasPrice.String(), ft.Denom)
		if err == nil {
			return Estimate{GasLimit: gas, Fee: fee}, nil
		}
	}

	// 4. Static defaults.
	if logger != nil {
		logger.Warn("tx: using static fee defaults; consider configuring fee_tokens",
			slog.String("chain_id", chain.ChainID))
	}
	baseGas := decimal.NewFromInt(int64(defaultBaseGas + defaultGasPerOutput*uint64(len(msgs))))
	gas := baseGas.Mul(gasAdjustCold).Ceil().BigInt().Uint64()
	fee, err := calcFee(gas, defaultGasPrice.String(), defaultFeeDenom)
	if err != nil {
		return Estimate{}, fmt.Errorf("tx: static fee fallback: %w", err)
	}
	return Estimate{GasLimit: gas, Fee: fee}, nil
}

// trySimulate calls Service/Simulate with a minimal placeholder tx.
// Returns (estimate, true, nil) on success, (zero, false, nil) if unavailable.
func trySimulate(
	ctx context.Context,
	txSvc txv1beta1.ServiceClient,
	chain *chainregistry.ChainInfo,
	msgs []*anypb.Any,
) (Estimate, bool, error) {
	body := &txv1beta1.TxBody{Messages: msgs}
	bodyBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(body)
	if err != nil {
		return Estimate{}, false, fmt.Errorf("tx: simulate marshal body: %w", err)
	}
	// gas_limit=0 signals simulate mode: the chain uses an infinite gas meter.
	// Without a non-nil Fee, cosmos-sdk's GetGas() nil-deferences AuthInfo.Fee.
	authInfoBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(
		&txv1beta1.AuthInfo{Fee: &txv1beta1.Fee{}},
	)
	if err != nil {
		return Estimate{}, false, fmt.Errorf("tx: simulate marshal auth_info: %w", err)
	}
	txBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(&txv1beta1.TxRaw{
		BodyBytes:     bodyBytes,
		AuthInfoBytes: authInfoBytes,
	})
	if err != nil {
		return Estimate{}, false, fmt.Errorf("tx: simulate marshal txraw: %w", err)
	}

	simResp, err := txSvc.Simulate(ctx, &txv1beta1.SimulateRequest{TxBytes: txBytes})
	if err != nil || simResp.GasInfo == nil {
		return Estimate{}, false, nil //nolint:nilerr // simulation is optional; any error means fall through to next strategy
	}

	gasUsed := decimal.NewFromInt(int64(simResp.GasInfo.GasUsed))
	gas := gasUsed.Mul(gasAdjustCold).Ceil().BigInt().Uint64()

	denom := defaultFeeDenom
	gasPriceStr := defaultGasPrice.String()
	if len(chain.FeeTokens) > 0 {
		denom = chain.FeeTokens[0].Denom
		gasPriceStr = chain.FeeTokens[0].AverageGasPrice.String()
	}

	fee, err := calcFee(gas, gasPriceStr, denom)
	if err != nil {
		return Estimate{}, false, err
	}
	return Estimate{GasLimit: gas, Fee: fee}, true, nil
}

// calcFee computes ceil(gasLimit × gasPriceStr) and returns a Coin.
func calcFee(gasLimit uint64, gasPriceStr, denom string) (Coin, error) {
	price, err := decimal.NewFromString(gasPriceStr)
	if err != nil {
		return Coin{}, fmt.Errorf("tx: parse gas price %q: %w", gasPriceStr, err)
	}
	amount := decimal.NewFromInt(int64(gasLimit)).Mul(price).Ceil()
	return Coin{Denom: denom, Amount: amount.String()}, nil
}
