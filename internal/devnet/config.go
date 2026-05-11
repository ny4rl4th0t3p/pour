package devnet

import (
	"fmt"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

// BuildConfig constructs a minimal standalone ChainsConfig in memory from
// genesis info. Used by --auto mode; no chains.yml is written or read.
//
// grpcAddr is the gRPC endpoint (e.g. "localhost:9090"); leave empty to omit.
// restAddr is the REST/LCD endpoint (e.g. "http://localhost:1317"); leave empty to omit.
// dripAmount is an optional coin string (e.g. "1000000uatom"); when empty,
// defaults to "1000000<denom>" and max_per_address_per_day to 10× that.
func BuildConfig(info *GenesisInfo, grpcAddr, restAddr, dripAmount string) (*config.ChainsConfig, error) {
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
	avgGasPrice := "0.025"

	return &config.ChainsConfig{
		Chains: []config.ChainConfig{
			{
				ChainID:      info.ChainID,
				Standalone:   true,
				Enabled:      &enabled,
				Bech32Prefix: &prefix,
				Slip44:       &slip44,
				FeeTokens: []config.FeeTokenConfig{
					{Denom: info.NativeDenom, AverageGasPrice: &avgGasPrice},
				},
				Endpoints:   buildEndpoints(grpcAddr, restAddr),
				BatchWindow: "0s",
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

func buildEndpoints(grpcAddr, restAddr string) *config.EndpointsConfig {
	ep := &config.EndpointsConfig{}
	if grpcAddr != "" {
		ep.GRPC = []string{grpcAddr}
	}
	if restAddr != "" {
		ep.REST = []string{restAddr}
	}
	return ep
}
