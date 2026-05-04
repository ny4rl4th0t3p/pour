package chainregistry

import (
	"testing"
)

func TestResolve_LiveOnlyLayer(t *testing.T) {
	s := newStoreV1(t)
	info, err := s.Get("test-alpha-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if info.ChainID != "test-alpha-1" {
		t.Errorf("ChainID: got %q", info.ChainID)
	}
	if info.Slip44 != 118 {
		t.Errorf("Slip44: got %d", info.Slip44)
	}
	if len(info.FeeTokens) != 1 || info.FeeTokens[0].Denom != "ualpha" {
		t.Errorf("FeeTokens: %v", info.FeeTokens)
	}
	if info.Sources.Identity != SourceLive {
		t.Errorf("Sources.Identity should be SourceLive")
	}
}

func TestResolve_LocalOverrideWins(t *testing.T) {
	grpcURL := "grpc.alpha-override.example:443"
	s, err := New(Options{
		Overrides: &OverrideSet{
			Chains: map[string]*ChainOverride{
				"test-alpha-1": {
					Endpoints: &EndpointsOverride{
						GRPC: []string{grpcURL},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/initial-v1.json"))
	if _, err := s.UpdateLive(snap); err != nil {
		t.Fatalf("UpdateLive: %v", err)
	}

	info, _ := s.Get("test-alpha-1")
	if len(info.Endpoints.GRPC) != 1 || info.Endpoints.GRPC[0].URL != grpcURL {
		t.Errorf("override not applied: %v", info.Endpoints.GRPC)
	}
	if info.Sources.Endpoints != SourceConfig {
		t.Errorf("Sources.Endpoints should be SourceConfig")
	}
}

func TestResolve_LocalOverrideBeatLive(t *testing.T) {
	// Local override must win even when a live snapshot also changes the field.
	grpcURL := "grpc.alpha-override.example:443"
	s, err := New(Options{
		Overrides: &OverrideSet{
			Chains: map[string]*ChainOverride{
				"test-alpha-1": {
					Endpoints: &EndpointsOverride{GRPC: []string{grpcURL}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/initial-v1.json"))
	if _, err := s.UpdateLive(snap); err != nil {
		t.Fatalf("UpdateLive: %v", err)
	}

	live := parseSnap(t, readTestFile(t, "testdata/snapshots/live-v1.json"))
	s.UpdateLive(live) //nolint:errcheck // ChangeSet return value not needed in this test

	info, _ := s.Get("test-alpha-1")
	if len(info.Endpoints.GRPC) == 0 || info.Endpoints.GRPC[0].URL != grpcURL {
		t.Errorf("local override should beat live; got %v", info.Endpoints.GRPC)
	}
	if info.Sources.Endpoints != SourceConfig {
		t.Errorf("Sources.Endpoints should remain SourceConfig")
	}
}

func TestResolve_FreezeDoesNotApplyLive(t *testing.T) {
	s := newStoreV1(t)
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/live-v1.json"))
	s.UpdateLive(snap) //nolint:errcheck // ChangeSet return value not needed in this test

	// live-v1.json sets bech32_prefix="beta2" for test-beta-1, which is Freeze policy.
	info, _ := s.Get("test-beta-1")
	if info.Bech32Prefix != "beta" {
		t.Errorf("Freeze field must not be applied until accepted; got %q", info.Bech32Prefix)
	}
}
