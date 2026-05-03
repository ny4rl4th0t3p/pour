package tx

import "testing"

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		addr    string
		prefix  string
		wantErr bool
	}{
		{"osmo1abc123defg", "osmo", false},
		{"cosmos1abc123defg", "cosmos", false},
		{"OSMO1ABC123DEFG", "osmo", false},  // case-insensitive
		{"osmo1abc123defg", "cosmos", true}, // wrong prefix
		{"osmo1", "osmo", true},             // empty data portion
		{"nope", "osmo", true},              // no '1' separator → sep=-1
		{"1abc123", "osmo", true},           // separator at position 0
		{"", "osmo", true},                  // empty string
		{"atom1abc123defg", "osmo", true},   // wrong prefix
	}
	for _, tt := range tests {
		t.Run(tt.addr+"_"+tt.prefix, func(t *testing.T) {
			err := ValidateAddress(tt.addr, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAddress(%q, %q) = %v, wantErr %v", tt.addr, tt.prefix, err, tt.wantErr)
			}
		})
	}
}
