package chainregistry

import (
	"testing"
)

// TestPolicyExhaustiveness asserts that classifiableFields (the operational list
// used by UpdateLive) and defaultFieldPolicy (the classification map) are in
// sync. Adding a field to ChainInfo requires updating both, and this test will
// fail until both are updated.
func TestPolicyExhaustiveness(t *testing.T) {
	fieldSet := make(map[string]bool, len(classifiableFields))
	for _, f := range classifiableFields {
		fieldSet[f] = true
	}

	// Every field in classifiableFields must have a policy.
	for _, f := range classifiableFields {
		if _, ok := defaultFieldPolicy[f]; !ok {
			t.Errorf("classifiable field %q is missing from defaultFieldPolicy", f)
		}
	}

	// Every policy entry must correspond to a classifiable field (no orphans).
	for key := range defaultFieldPolicy {
		if !fieldSet[key] {
			t.Errorf("defaultFieldPolicy key %q has no entry in classifiableFields", key)
		}
	}
}

func TestClassify_UnknownDefaultsToFreeze(t *testing.T) {
	if classify("SomeFutureField") != FieldPolicyFreeze {
		t.Error("unknown field should default to FieldPolicyFreeze")
	}
}

func TestClassify_KnownFields(t *testing.T) {
	cases := []struct {
		field string
		want  FieldPolicy
	}{
		{"Endpoints.GRPC", FieldPolicyHotReload},
		{"PrettyName", FieldPolicyHotReload},
		{"FeeTokens.Display", FieldPolicyHotReload},
		{"FeeTokens.AverageGasPrice", FieldPolicyWarn},
		{"FeeTokens.LowGasPrice", FieldPolicyWarn},
		{"FeeTokens.HighGasPrice", FieldPolicyWarn},
		{"Bech32Prefix", FieldPolicyFreeze},
		{"ChainID", FieldPolicyFreeze},
		{"Slip44", FieldPolicyFreeze},
		{"FeeTokens.Denom", FieldPolicyFreeze},
	}
	for _, tc := range cases {
		got := classify(tc.field)
		if got != tc.want {
			t.Errorf("classify(%q) = %v, want %v", tc.field, got, tc.want)
		}
	}
}
