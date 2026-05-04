package main

import (
	"testing"
)

func TestChainsValidateCmd(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "valid registry chain",
			config:  "testdata/valid-registry.yml",
			wantErr: false,
		},
		{
			name:    "valid standalone chain",
			config:  "testdata/valid-standalone.yml",
			wantErr: false,
		},
		{
			name:    "missing drip.anonymous",
			config:  "testdata/invalid-missing-drip.yml",
			wantErr: true,
		},
		{
			name:    "standalone missing bech32_prefix",
			config:  "testdata/invalid-standalone-no-bech32.yml",
			wantErr: true,
		},
		{
			name:    "file not found",
			config:  "testdata/does-not-exist.yml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &ChainsValidateCmd{Config: tt.config}
			err := cmd.Run()
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
