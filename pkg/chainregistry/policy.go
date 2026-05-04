package chainregistry

import "time"

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
	"ChainID":      FieldPolicyFreeze,
	"ChainName":    FieldPolicyFreeze,
	"NetworkType":  FieldPolicyFreeze,
	"Bech32Prefix": FieldPolicyFreeze,
	"Slip44":       FieldPolicyFreeze,
	"KeyAlgo":      FieldPolicyFreeze,

	// FeeToken denom change is a hard event: existing gas-cache entries become
	// stale and transactions built with the old denom will be rejected.
	"FeeTokens.Denom": FieldPolicyFreeze,

	// Gas price changes are operationally significant but not breaking — apply
	// immediately and warn so operators can review.
	"FeeTokens.LowGasPrice":     FieldPolicyWarn,
	"FeeTokens.AverageGasPrice": FieldPolicyWarn,
	"FeeTokens.HighGasPrice":    FieldPolicyWarn,

	// Display metadata and exponent are purely informational.
	"FeeTokens.Display":  FieldPolicyHotReload,
	"FeeTokens.Exponent": FieldPolicyHotReload,

	// Endpoint list changes are routine: registries update these frequently.
	"Endpoints.GRPC": FieldPolicyHotReload,
	"Endpoints.RPC":  FieldPolicyHotReload,
	"Endpoints.REST": FieldPolicyHotReload,

	// Cosmetic and operational metadata.
	"PrettyName": FieldPolicyHotReload,
	"BlockTime":  FieldPolicyHotReload,
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
