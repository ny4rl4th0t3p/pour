package chainregistry

import (
	"slices"
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
	if ov.Enabled != nil {
		info.Enabled = *ov.Enabled
	}
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
	applyEndpoints(info, sources, ov.Endpoints)
	applyFeeTokens(info, sources, ov.FeeTokens)
}

func applyEndpoints(info *ChainInfo, sources *FieldSources, ov *EndpointsOverride) {
	if ov == nil {
		return
	}
	if ov.GRPC != nil {
		info.Endpoints.GRPC = stringsToEndpoints(ov.GRPC)
		sources.Endpoints = SourceConfig
	}
	if ov.RPC != nil {
		info.Endpoints.RPC = stringsToEndpoints(ov.RPC)
		sources.Endpoints = SourceConfig
	}
	if ov.REST != nil {
		info.Endpoints.REST = stringsToEndpoints(ov.REST)
		sources.Endpoints = SourceConfig
	}
}

func applyFeeTokens(info *ChainInfo, sources *FieldSources, overrides []FeeTokenOverride) {
	for _, ov := range overrides {
		idx := slices.IndexFunc(info.FeeTokens, func(ft FeeToken) bool { return ft.Denom == ov.Denom })
		if idx < 0 {
			continue
		}
		applyFeeToken(&info.FeeTokens[idx], &sources.FeeTokens, ov)
	}
}

func applyFeeToken(ft *FeeToken, src *Source, ov FeeTokenOverride) {
	applyDecimalField(&ft.LowGasPrice, src, ov.LowGasPrice)
	applyDecimalField(&ft.AverageGasPrice, src, ov.AverageGasPrice)
	applyDecimalField(&ft.HighGasPrice, src, ov.HighGasPrice)
}

func applyDecimalField(field *decimal.Decimal, src *Source, raw *string) {
	if raw == nil {
		return
	}
	if d, err := decimal.NewFromString(*raw); err == nil {
		*field = d
		*src = SourceConfig
	}
}

func stringsToEndpoints(ss []string) []Endpoint {
	out := make([]Endpoint, 0, len(ss))
	for _, s := range ss {
		out = append(out, Endpoint{URL: s})
	}
	return out
}
