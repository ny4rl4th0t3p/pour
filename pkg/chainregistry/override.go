package chainregistry

import (
	"time"

	"github.com/shopspring/decimal"
)

// OverrideSet is the daemon's chains.yml parsed into a typed form.
// pkg/chainregistry is YAML-agnostic; the daemon builds this via
// config.ChainsConfig.ToOverrideSet() and passes it to New or SetOverrides.
type OverrideSet struct {
	Chains map[string]*ChainOverride
}

// ChainOverride holds operator-supplied overrides for a single chain.
// Pointer fields distinguish "not overridden" (nil) from "explicitly set".
// Identity fields (ChainName, NetworkType, KeyAlgo, Bech32Prefix, Slip44) are
// primarily used by standalone chains, which must supply them in full since
// there is no registry to fall back to.
type ChainOverride struct {
	// Enabled controls whether the daemon runs this chain.
	Enabled *bool

	// Identity overrides.
	ChainName    *string
	NetworkType  *NetworkType
	KeyAlgo      *KeyAlgo
	Bech32Prefix *string
	Slip44       *uint32

	Endpoints *EndpointsOverride
	FeeTokens []FeeTokenOverride
	BlockTime *time.Duration

	// Drip is required for chains the operator enables for serving.
	Drip DripPolicy

	// Distributors and BatchWindow are v0.3.0 fields; stored here so chains.yml
	// can include them without causing parse errors.
	Distributors int
	BatchWindow  time.Duration
}

// EndpointsOverride replaces the full protocol list when non-nil.
// A non-nil slice fully replaces the registry-provided list for that protocol;
// it does not merge. nil means "inherit from registry".
type EndpointsOverride struct {
	GRPC []string
	RPC  []string
	REST []string
}

// FeeTokenOverride overrides specific gas price tiers for a single fee token,
// identified by Denom. nil pointer fields inherit the registry value.
type FeeTokenOverride struct {
	Denom           string
	LowGasPrice     *string
	AverageGasPrice *string
	HighGasPrice    *string
}

// DripPolicy holds per-chain drip amounts. Anonymous and MaxPerAddressPerDay
// are required for chains the operator enables.
type DripPolicy struct {
	Anonymous           string
	Signed              string
	MaxPerAddressPerDay string
	Memo                string
}

// applyOverride merges ov into info unconditionally (always wins over registry data).
// nil pointer fields in ov are left untouched on info.
func applyOverride(info *ChainInfo, sources *FieldSources, ov *ChainOverride) {
	if ov.ChainName != nil {
		info.ChainName = *ov.ChainName
		sources.Identity = SourceConfig
	}
	if ov.NetworkType != nil {
		info.NetworkType = *ov.NetworkType
		sources.Identity = SourceConfig
	}
	if ov.KeyAlgo != nil {
		info.KeyAlgo = *ov.KeyAlgo
		sources.Address = SourceConfig
	}
	if ov.Bech32Prefix != nil {
		info.Bech32Prefix = *ov.Bech32Prefix
		sources.Address = SourceConfig
	}
	if ov.Slip44 != nil {
		info.Slip44 = *ov.Slip44
		sources.Address = SourceConfig
	}
	if ov.BlockTime != nil {
		info.BlockTime = *ov.BlockTime
		sources.BlockTime = SourceConfig
	}
	if ov.Endpoints != nil {
		if ov.Endpoints.GRPC != nil {
			info.Endpoints.GRPC = stringsToEndpoints(ov.Endpoints.GRPC)
			sources.Endpoints = SourceConfig
		}
		if ov.Endpoints.RPC != nil {
			info.Endpoints.RPC = stringsToEndpoints(ov.Endpoints.RPC)
			sources.Endpoints = SourceConfig
		}
		if ov.Endpoints.REST != nil {
			info.Endpoints.REST = stringsToEndpoints(ov.Endpoints.REST)
			sources.Endpoints = SourceConfig
		}
	}
	for _, ft := range ov.FeeTokens {
		for i := range info.FeeTokens {
			if info.FeeTokens[i].Denom != ft.Denom {
				continue
			}
			if ft.LowGasPrice != nil {
				if d, err := decimal.NewFromString(*ft.LowGasPrice); err == nil {
					info.FeeTokens[i].LowGasPrice = d
					sources.FeeTokens = SourceConfig
				}
			}
			if ft.AverageGasPrice != nil {
				if d, err := decimal.NewFromString(*ft.AverageGasPrice); err == nil {
					info.FeeTokens[i].AverageGasPrice = d
					sources.FeeTokens = SourceConfig
				}
			}
			if ft.HighGasPrice != nil {
				if d, err := decimal.NewFromString(*ft.HighGasPrice); err == nil {
					info.FeeTokens[i].HighGasPrice = d
					sources.FeeTokens = SourceConfig
				}
			}
		}
	}
}

func stringsToEndpoints(ss []string) []Endpoint {
	out := make([]Endpoint, 0, len(ss))
	for _, s := range ss {
		out = append(out, Endpoint{URL: s})
	}
	return out
}
