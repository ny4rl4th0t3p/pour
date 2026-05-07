package chainregistry

import (
	"os"
	"testing"
)

func TestIBCPairFilename(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"osmosis", "cosmoshub", "cosmoshub-osmosis.json"},
		{"cosmoshub", "osmosis", "cosmoshub-osmosis.json"}, // order-independent
		{"neutron", "axelar", "axelar-neutron.json"},
		{"a", "a", "a-a.json"},
	}
	for _, tc := range tests {
		got := ibcPairFilename(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("ibcPairFilename(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseIBCFile_FiltersAndMaps(t *testing.T) {
	data, err := os.ReadFile("testdata/ibc/osmosis-cosmoshub.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	channels, err := unmarshalIBCFile(data)
	if err != nil {
		t.Fatalf("unmarshalIBCFile: %v", err)
	}

	// ics27-1 channel must be filtered out; only 2 ics20-1 channels remain.
	if len(channels) != 2 {
		t.Fatalf("expected 2 ICS20 channels, got %d", len(channels))
	}

	// First channel: preferred, live.
	ch := channels[0]
	if ch.ChainNameA != "cosmoshub" {
		t.Errorf("ChainNameA = %q, want %q", ch.ChainNameA, "cosmoshub")
	}
	if ch.ChainNameB != "osmosis" {
		t.Errorf("ChainNameB = %q, want %q", ch.ChainNameB, "osmosis")
	}
	if ch.ChannelA != "channel-141" {
		t.Errorf("ChannelA = %q, want %q", ch.ChannelA, "channel-141")
	}
	if ch.ChannelB != "channel-0" {
		t.Errorf("ChannelB = %q, want %q", ch.ChannelB, "channel-0")
	}
	if ch.PortA != "transfer" || ch.PortB != "transfer" {
		t.Errorf("ports = %q/%q, want transfer/transfer", ch.PortA, ch.PortB)
	}
	if ch.Status != "live" {
		t.Errorf("Status = %q, want %q", ch.Status, "live")
	}
	if !ch.Preferred {
		t.Error("expected Preferred = true for first channel")
	}

	// Second channel: live but not preferred.
	ch2 := channels[1]
	if ch2.Preferred {
		t.Error("expected Preferred = false for second channel")
	}
	if ch2.ChannelA != "channel-569" {
		t.Errorf("ChannelA = %q, want %q", ch2.ChannelA, "channel-569")
	}
}

func TestIBCChannelFor(t *testing.T) {
	ch := IBCChannel{
		ChainNameA: "cosmoshub",
		ChainNameB: "osmosis",
		ChannelA:   "channel-141",
		ChannelB:   "channel-0",
		PortA:      "transfer",
		PortB:      "transfer",
	}

	tests := []struct {
		name          string
		wantChannelID string
		wantPort      string
		wantPeer      string
		wantOK        bool
	}{
		{"cosmoshub", "channel-141", "transfer", "osmosis", true},
		{"osmosis", "channel-0", "transfer", "cosmoshub", true},
		{"neutron", "", "", "", false},
	}
	for _, tc := range tests {
		gotCh, gotPort, gotPeer, gotOK := ch.ChannelFor(tc.name)
		if gotOK != tc.wantOK {
			t.Errorf("ChannelFor(%q) ok = %v, want %v", tc.name, gotOK, tc.wantOK)
			continue
		}
		if gotCh != tc.wantChannelID || gotPort != tc.wantPort || gotPeer != tc.wantPeer {
			t.Errorf("ChannelFor(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.name, gotCh, gotPort, gotPeer,
				tc.wantChannelID, tc.wantPort, tc.wantPeer)
		}
	}
}

func TestParseIBCFile_Empty(t *testing.T) {
	channels, err := unmarshalIBCFile([]byte(`{"chain_1":{"chain_name":"a"},"chain_2":{"chain_name":"b"},"channels":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(channels))
	}
}

func TestSelectChannel(t *testing.T) {
	channels := []IBCChannel{
		{
			ChainNameA: "cosmoshub", ChainNameB: "osmosis",
			ChannelA: "channel-141", ChannelB: "channel-0",
			PortA: "transfer", PortB: "transfer",
			Version: "ics20-1", Status: "live", Preferred: true,
		},
		{
			ChainNameA: "cosmoshub", ChainNameB: "osmosis",
			ChannelA: "channel-569", ChannelB: "channel-362",
			PortA: "transfer", PortB: "transfer",
			Version: "ics20-1", Status: "live", Preferred: false,
		},
		{
			ChainNameA: "cosmoshub", ChainNameB: "osmosis",
			ChannelA: "channel-777", ChannelB: "channel-888",
			PortA: "transfer", PortB: "transfer",
			Version: "ics20-1", Status: "upcoming", Preferred: false,
		},
	}

	// Preferred live channel wins.
	ch, ok, ambiguous := SelectChannel(channels, "cosmoshub", "osmosis")
	if !ok {
		t.Fatal("SelectChannel: expected ok=true")
	}
	if ambiguous {
		t.Error("SelectChannel: expected ambiguous=false when preferred channel exists")
	}
	if ch.ChannelA != "channel-141" {
		t.Errorf("ChannelA = %q, want channel-141", ch.ChannelA)
	}

	// Works from either direction.
	ch2, ok2, _ := SelectChannel(channels, "osmosis", "cosmoshub")
	if !ok2 || ch2.ChannelB != "channel-0" {
		t.Errorf("reverse SelectChannel failed: ok=%v channelB=%q", ok2, ch2.ChannelB)
	}

	// No channel for an unknown pair.
	_, ok3, _ := SelectChannel(channels, "neutron", "osmosis")
	if ok3 {
		t.Error("expected ok=false for unknown pair")
	}
}

func TestSelectChannel_AmbiguousWithoutPreferred(t *testing.T) {
	channels := []IBCChannel{
		{
			ChainNameA: "a", ChainNameB: "b",
			ChannelA: "channel-0", ChannelB: "channel-1",
			Version: "ics20-1", Status: "live", Preferred: false,
		},
		{
			ChainNameA: "a", ChainNameB: "b",
			ChannelA: "channel-2", ChannelB: "channel-3",
			Version: "ics20-1", Status: "live", Preferred: false,
		},
	}
	ch, ok, ambiguous := SelectChannel(channels, "a", "b")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !ambiguous {
		t.Error("expected ambiguous=true when multiple live channels and none preferred")
	}
	// Falls back to first live channel.
	if ch.ChannelA != "channel-0" {
		t.Errorf("ChannelA = %q, want channel-0", ch.ChannelA)
	}
}

func TestParseIBCFile_NoICS20(t *testing.T) {
	data := `{
		"chain_1":{"chain_name":"a"},"chain_2":{"chain_name":"b"},
		"channels":[
			{"chain_1":{"channel_id":"ch-1","port_id":"icahost"},
			 "chain_2":{"channel_id":"ch-2","port_id":"icahost"},
			 "ordering":"ordered","version":"ics27-1",
			 "tags":{"status":"live","preferred":false}}
		]}`
	channels, err := unmarshalIBCFile([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 ICS20 channels after filtering ics27-1, got %d", len(channels))
	}
}
