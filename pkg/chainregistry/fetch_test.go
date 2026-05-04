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
