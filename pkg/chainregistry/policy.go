package chainregistry

import "time"

// Field name constants used in fieldValues, applyAcceptedField, defaultFieldPolicy,
// and classifiableFields. Exported so internal/chain can reference them when logging
// ChangeSets without importing string literals.
const (
	FieldChainID               = "ChainID"
	FieldChainName             = "ChainName"
	FieldNetworkType           = "NetworkType"
	FieldPrettyName            = "PrettyName"
	FieldBech32Prefix          = "Bech32Prefix"
	FieldSlip44                = "Slip44"
	FieldKeyAlgo               = "KeyAlgo"
	FieldEndpointsGRPC         = "Endpoints.GRPC"
	FieldEndpointsRPC          = "Endpoints.RPC"
	FieldEndpointsREST         = "Endpoints.REST"
	FieldBlockTime             = "BlockTime"
	FieldFeeTokensDenom        = "FeeTokens.Denom"           //nolint:gosec // registry field path, not a credential
	FieldFeeTokensLowGasPrice  = "FeeTokens.LowGasPrice"     //nolint:gosec // registry field path, not a credential
	FieldFeeTokensAvgGasPrice  = "FeeTokens.AverageGasPrice" //nolint:gosec // registry field path, not a credential
	FieldFeeTokensHighGasPrice = "FeeTokens.HighGasPrice"    //nolint:gosec // registry field path, not a credential
	FieldFeeTokensDisplay      = "FeeTokens.Display"         //nolint:gosec // registry field path, not a credential
	FieldFeeTokensExponent     = "FeeTokens.Exponent"        //nolint:gosec // registry field path, not a credential
)

// FieldPolicy classifies how a live registry update to a field is handled.
type FieldPolicy int

const (
	// FieldPolicyHotReload silently applies live updates to the resolved view.
	FieldPolicyHotReload FieldPolicy = iota

	// FieldPolicyWarn applies live updates but signals the caller to log a warning.
	FieldPolicyWarn

	// FieldPolicyFreeze blocks live updates and queues a PendingChange for
	// operator acceptance via Accept.
	FieldPolicyFreeze
)

// defaultFieldPolicy is the single source of truth for how each ChainInfo field
// responds to live registry updates. policy_test.go asserts that every
// classifiable field in ChainInfo has an entry here (exhaustiveness check).
var defaultFieldPolicy = map[string]FieldPolicy{
	// Identity-affecting fields: changing these can break address derivation or
	// transaction signing — require explicit operator acknowledgment.
	FieldChainID:      FieldPolicyFreeze,
	FieldChainName:    FieldPolicyFreeze,
	FieldNetworkType:  FieldPolicyFreeze,
	FieldBech32Prefix: FieldPolicyFreeze,
	FieldSlip44:       FieldPolicyFreeze,
	FieldKeyAlgo:      FieldPolicyFreeze,

	// FeeToken denom change is a hard event: existing gas-cache entries become
	// stale and transactions built with the old denom will be rejected.
	FieldFeeTokensDenom: FieldPolicyFreeze,

	// Gas price changes are operationally significant but not breaking — apply
	// immediately and warn so operators can review.
	FieldFeeTokensLowGasPrice:  FieldPolicyWarn,
	FieldFeeTokensAvgGasPrice:  FieldPolicyWarn,
	FieldFeeTokensHighGasPrice: FieldPolicyWarn,

	// Display metadata and exponent are purely informational.
	FieldFeeTokensDisplay:  FieldPolicyHotReload,
	FieldFeeTokensExponent: FieldPolicyHotReload,

	// Endpoint list changes are routine: registries update these frequently.
	FieldEndpointsGRPC: FieldPolicyHotReload,
	FieldEndpointsRPC:  FieldPolicyHotReload,
	FieldEndpointsREST: FieldPolicyHotReload,

	// Cosmetic and operational metadata.
	FieldPrettyName: FieldPolicyHotReload,
	FieldBlockTime:  FieldPolicyHotReload,
}

// classify returns the policy for a named field path (e.g. "FeeTokens.Denom").
// Unrecognized fields default to FieldPolicyFreeze — the safe choice.
func classify(field string) FieldPolicy {
	if p, ok := defaultFieldPolicy[field]; ok {
		return p
	}
	return FieldPolicyFreeze
}

// PendingChange records a freeze-policy field change detected from the live
// registry that is awaiting operator acceptance via Store.Accept.
type PendingChange struct {
	ChainID    string
	Field      string
	OldValue   any
	NewValue   any
	DetectedAt time.Time
	Source     Source
}
