package chainregistry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func chainJSON(chainID, chainName, bech32 string) []byte {
	b, _ := json.Marshal(rawChainInfo{
		ChainID:      chainID,
		ChainName:    chainName,
		NetworkType:  "testnet",
		Bech32Prefix: bech32,
		Slip44:       118,
		KeyAlgos:     []string{"secp256k1"},
	})
	return b
}

func TestChainNameFromID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"osmosis-1", "osmosis"},
		{"cosmoshub-4", "cosmoshub"},
		{"test-alpha-1", "test-alpha"},
		{"mynet-1", "mynet"},
		{"nodigits", "nodigits"},
		{"dash-123", "dash"},
	}
	for _, tc := range cases {
		if got := ChainNameFromID(tc.in); got != tc.want {
			t.Errorf("ChainNameFromID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFetchLive_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test-alpha/chain.json":
			w.Write(chainJSON("test-alpha-1", "testalpha", "alpha")) //nolint:errcheck // test response write
		case "/test-beta/chain.json":
			w.Write(chainJSON("test-beta-1", "testbeta", "beta")) //nolint:errcheck // test response write
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	snap, err := FetchLive(t.Context(), FetchOptions{
		BaseURL:  srv.URL,
		ChainIDs: []string{"test-alpha-1", "test-beta-1"},
	})
	if err != nil {
		t.Fatalf("FetchLive: %v", err)
	}
	if len(snap.Chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(snap.Chains))
	}
	if snap.Chains["test-alpha-1"].Bech32Prefix != "alpha" {
		t.Errorf("test-alpha-1 Bech32Prefix = %q, want %q", snap.Chains["test-alpha-1"].Bech32Prefix, "alpha")
	}
	if snap.Chains["test-beta-1"].Bech32Prefix != "beta" {
		t.Errorf("test-beta-1 Bech32Prefix = %q, want %q", snap.Chains["test-beta-1"].Bech32Prefix, "beta")
	}
}

func TestFetchLive_404ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := FetchLive(t.Context(), FetchOptions{
		BaseURL:  srv.URL,
		ChainIDs: []string{"osmosis-1"},
	})
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestFetchLive_PartialFailureReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-alpha/chain.json" {
			w.Write(chainJSON("test-alpha-1", "testalpha", "alpha")) //nolint:errcheck // test response write
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := FetchLive(t.Context(), FetchOptions{
		BaseURL:  srv.URL,
		ChainIDs: []string{"test-alpha-1", "test-beta-1"},
	})
	if err == nil {
		t.Fatal("expected error when one chain fails, got nil")
	}
}

func ibcJSON(nameA, chanA, nameB, chanB string) []byte {
	b, _ := json.Marshal(rawIBCFile{
		Chain1: rawIBCSide{ChainName: nameA},
		Chain2: rawIBCSide{ChainName: nameB},
		Channels: []rawIBCChannel{{
			Chain1:   rawIBCChannelSide{ChannelID: chanA, PortID: "transfer"},
			Chain2:   rawIBCChannelSide{ChannelID: chanB, PortID: "transfer"},
			Ordering: "unordered",
			Version:  "ics20-1",
			Tags:     rawIBCTags{Status: "live", Preferred: true},
		}},
	})
	return b
}

func TestFetchIBCChannels_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_IBC/test-alpha-test-beta.json" {
			w.Write(ibcJSON("test-alpha", "channel-0", "test-beta", "channel-1")) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	channels, errs := FetchIBCChannels(t.Context(), srv.URL, [][2]string{{"test-alpha", "test-beta"}}, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	ch := channels[0]
	if ch.ChannelA != "channel-0" || ch.ChannelB != "channel-1" {
		t.Errorf("channels = %q/%q, want channel-0/channel-1", ch.ChannelA, ch.ChannelB)
	}
}

func TestFetchIBCChannels_404Skipped(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	channels, errs := FetchIBCChannels(t.Context(), srv.URL, [][2]string{{"a", "b"}}, nil)
	if len(errs) != 0 {
		t.Fatalf("404 should be non-fatal, got errors: %v", errs)
	}
	if len(channels) != 0 {
		t.Fatalf("expected 0 channels for 404 pair, got %d", len(channels))
	}
}

func TestFetchIBCChannels_ServerErrorReturnsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, errs := FetchIBCChannels(t.Context(), srv.URL, [][2]string{{"a", "b"}}, nil)
	if len(errs) == 0 {
		t.Fatal("expected error for 500, got none")
	}
}

func TestFetchLive_PopulatesIBCChannels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test-alpha/chain.json":
			w.Write(chainJSON("test-alpha-1", "test-alpha", "alpha")) //nolint:errcheck
		case "/test-beta/chain.json":
			w.Write(chainJSON("test-beta-1", "test-beta", "beta")) //nolint:errcheck
		case "/_IBC/test-alpha-test-beta.json":
			w.Write(ibcJSON("test-alpha", "channel-0", "test-beta", "channel-1")) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	snap, err := FetchLive(t.Context(), FetchOptions{
		BaseURL:  srv.URL,
		ChainIDs: []string{"test-alpha-1", "test-beta-1"},
	})
	if err != nil {
		t.Fatalf("FetchLive: %v", err)
	}
	if len(snap.IBCChannels) != 1 {
		t.Fatalf("expected 1 IBC channel, got %d", len(snap.IBCChannels))
	}
	if snap.IBCChannels[0].ChannelA != "channel-0" {
		t.Errorf("ChannelA = %q, want channel-0", snap.IBCChannels[0].ChannelA)
	}
}

func TestChainNamePairs(t *testing.T) {
	pairs := chainNamePairs([]string{"osmosis-1", "cosmoshub-4", "neutron-1"})
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs for 3 chains, got %d", len(pairs))
	}
	// All pairs are unique.
	seen := make(map[[2]string]bool)
	for _, p := range pairs {
		if seen[p] {
			t.Errorf("duplicate pair: %v", p)
		}
		seen[p] = true
	}
}

func TestChainNamePairs_Single(t *testing.T) {
	pairs := chainNamePairs([]string{"osmosis-1"})
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for 1 chain, got %d", len(pairs))
	}
}
