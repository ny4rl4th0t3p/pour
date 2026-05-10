package devnet

import (
	"fmt"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

// BuildConfig constructs a minimal standalone ChainsConfig in memory from
// genesis info. Used by --auto mode; no chains.yml is written or read.
//
// grpcAddr is the gRPC endpoint for the chain (e.g. "localhost:9090").
// dripAmount is an optional coin string (e.g. "1000000uatom"); when empty,
// defaults to "1000000<denom>" and max_per_address_per_day to 10× that.
func BuildConfig(info *GenesisInfo, grpcAddr, dripAmount string) (*config.ChainsConfig, error) {
	if info.NativeDenom == "" {
		return nil, fmt.Errorf("devnet: could not determine native denom from genesis; set --drip explicitly")
	}

	if dripAmount == "" {
		dripAmount = fmt.Sprintf("1000000%s", info.NativeDenom)
	}
	maxDrip := fmt.Sprintf("10000000%s", info.NativeDenom)

	const defaultSlip44 = uint32(118)
	enabled := true
	prefix := info.Bech32Prefix
	slip44 := defaultSlip44

	return &config.ChainsConfig{
		Chains: []config.ChainConfig{
			{
				ChainID:      info.ChainID,
				Standalone:   true,
				Enabled:      &enabled,
				Bech32Prefix: &prefix,
				Slip44:       &slip44,
				FeeTokens: []config.FeeTokenConfig{
					{Denom: info.NativeDenom},
				},
				Endpoints: &config.EndpointsConfig{
					GRPC: []string{grpcAddr},
				},
				BatchWindow: "0s", // synchronous mode — devnets don't need batching
				Drip: config.DripConfig{
					Anonymous:           dripAmount,
					MaxPerAddressPerDay: maxDrip,
				},
				IBC: config.IBCConfig{
					Timeout: "10m",
				},
			},
		},
	}, nil
}
