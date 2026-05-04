package chainregistry

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func parseSnap(t *testing.T, data []byte) *Snapshot {
	t.Helper()
	var raw rawSnapshot
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	return &Snapshot{Chains: raw.Chains}
}

// newStoreV1 creates a store pre-populated with the initial test snapshot.
// This is the baseline for tests that need an established state to update from.
func newStoreV1(t *testing.T) *Store {
	t.Helper()
	s, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.AddStandalone(ChainInfo{ChainID: "mynet-1", Bech32Prefix: "mynet", Slip44: 118})
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/initial-v1.json"))
	if _, err := s.UpdateLive(snap); err != nil {
		t.Fatalf("UpdateLive: %v", err)
	}
	return s
}

func TestNew_EmptyStore(t *testing.T) {
	s, err := New(Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.IDs()) != 0 {
		t.Fatalf("expected empty store, got %v", s.IDs())
	}
}

func TestUpdateLive_PopulatesChains(t *testing.T) {
	s := newStoreV1(t)
	ids := s.IDs()
	if len(ids) != 3 {
		t.Fatalf("expected 3 chains, got %d: %v", len(ids), ids)
	}
	if ids[0] != "mynet-1" || ids[1] != "test-alpha-1" || ids[2] != "test-beta-1" {
		t.Fatalf("unexpected IDs: %v", ids)
	}
}

func TestGet_Found(t *testing.T) {
	s := newStoreV1(t)
	info, err := s.Get("test-alpha-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if info.Bech32Prefix != "alpha" {
		t.Errorf("Bech32Prefix: got %q, want %q", info.Bech32Prefix, "alpha")
	}
	if info.Sources.Identity != SourceLive {
		t.Errorf("Sources.Identity: got %v, want SourceLive", info.Sources.Identity)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newStoreV1(t)
	_, err := s.Get("nonexistent-1")
	if !errors.Is(err, ErrChainNotFound) {
		t.Fatalf("expected ErrChainNotFound, got %v", err)
	}
}

func TestGet_ReturnsSamePointer(t *testing.T) {
	s := newStoreV1(t)
	p1, _ := s.Get("test-alpha-1")
	p2, _ := s.Get("test-alpha-1")
	if p1 != p2 {
		t.Error("expected same pointer on repeated Get calls (no mutation between calls)")
	}
}

func TestList_Sorted(t *testing.T) {
	s := newStoreV1(t)
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if list[0].ChainID != "mynet-1" || list[1].ChainID != "test-alpha-1" || list[2].ChainID != "test-beta-1" {
		t.Errorf("unexpected order: %v %v %v", list[0].ChainID, list[1].ChainID, list[2].ChainID)
	}
}

func TestAddStandalone_PopulatesChain(t *testing.T) {
	s, _ := New(Options{})
	s.AddStandalone(ChainInfo{
		ChainID:      "mynet-1",
		Bech32Prefix: "mynet",
		Slip44:       118,
	})
	info, err := s.Get("mynet-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if info.Bech32Prefix != "mynet" {
		t.Errorf("Bech32Prefix: got %q", info.Bech32Prefix)
	}
	if info.Sources.Identity != SourceConfig {
		t.Errorf("Sources.Identity: got %v, want SourceConfig", info.Sources.Identity)
	}
}

func TestAddStandalone_NotOverwrittenByUpdateLive(t *testing.T) {
	s, _ := New(Options{})
	s.AddStandalone(ChainInfo{
		ChainID:      "mynet-1",
		Bech32Prefix: "mynet",
		Slip44:       118,
	})
	// A live snapshot that also contains mynet-1 should not overwrite it.
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/initial-v1.json"))
	snap.Chains["mynet-1"] = snap.Chains["test-alpha-1"] // inject a registry entry
	s.UpdateLive(snap)                                   //nolint:errcheck // not relevant here
	info, _ := s.Get("mynet-1")
	if info.Sources.Identity != SourceConfig {
		t.Errorf("standalone chain should not be overwritten by UpdateLive")
	}
}

func TestUpdateLive_HotReloadApplied(t *testing.T) {
	s := newStoreV1(t)
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/live-v1.json"))

	cs, err := s.UpdateLive(snap)
	if err != nil {
		t.Fatalf("UpdateLive: %v", err)
	}

	// gRPC endpoint changed (HotReload) — must be applied immediately.
	info, _ := s.Get("test-alpha-1")
	if len(info.Endpoints.GRPC) == 0 || info.Endpoints.GRPC[0].URL != "grpc.alpha-new.example:443" {
		t.Errorf("gRPC endpoint not updated: %v", info.Endpoints.GRPC)
	}
	if cs.Empty() {
		t.Error("expected non-empty ChangeSet")
	}
}

func TestUpdateLive_WarnFieldApplied(t *testing.T) {
	s := newStoreV1(t)
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/live-v1.json"))
	s.UpdateLive(snap) //nolint:errcheck // ChangeSet return value not needed in this test

	// test-alpha-1 average gas price changed (Warn) — applied but flagged.
	info, _ := s.Get("test-alpha-1")
	if info.FeeTokens[0].AverageGasPrice.String() != "0.02" {
		t.Errorf("AverageGasPrice: got %v, want 0.02", info.FeeTokens[0].AverageGasPrice)
	}
}

func TestUpdateLive_FreezeFieldEnqueuesPending(t *testing.T) {
	s := newStoreV1(t)
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/live-v1.json"))
	s.UpdateLive(snap) //nolint:errcheck // ChangeSet return value not needed in this test

	// test-beta-1 bech32_prefix changed (Freeze) — not applied, queued as pending.
	info, _ := s.Get("test-beta-1")
	if info.Bech32Prefix != "beta" {
		t.Errorf("Bech32Prefix should not change for Freeze policy: got %q", info.Bech32Prefix)
	}
	pending := s.Pending()
	found := false
	for _, pc := range pending {
		if pc.ChainID == "test-beta-1" && pc.Field == "Bech32Prefix" {
			found = true
			if pc.NewValue != "beta2" {
				t.Errorf("PendingChange.NewValue: got %v, want %q", pc.NewValue, "beta2")
			}
		}
	}
	if !found {
		t.Error("expected pending change for test-beta-1 Bech32Prefix")
	}
}

func TestAccept_AppliesAndRemovesPending(t *testing.T) {
	s := newStoreV1(t)
	snap := parseSnap(t, readTestFile(t, "testdata/snapshots/live-v1.json"))
	s.UpdateLive(snap) //nolint:errcheck // ChangeSet return value not needed in this test

	if err := s.Accept("test-beta-1", "Bech32Prefix"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	info, _ := s.Get("test-beta-1")
	if info.Bech32Prefix != "beta2" {
		t.Errorf("after Accept, Bech32Prefix = %q, want %q", info.Bech32Prefix, "beta2")
	}
	for _, pc := range s.Pending() {
		if pc.ChainID == "test-beta-1" && pc.Field == "Bech32Prefix" {
			t.Error("pending change should be removed after Accept")
		}
	}
}

func TestAccept_NoPendingChange(t *testing.T) {
	s := newStoreV1(t)
	err := s.Accept("test-alpha-1", "Bech32Prefix")
	if err == nil {
		t.Fatal("expected error for non-existent pending change")
	}
}
