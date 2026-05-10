package chain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

// statusServer returns an HTTP handler that serves successive block heights from
// the heights slice. Once exhausted it keeps serving the last value.
func statusServer(heights []int64) http.HandlerFunc {
	var call atomic.Int64
	return func(w http.ResponseWriter, _ *http.Request) {
		idx := call.Add(1) - 1
		h := heights[min(int(idx), len(heights)-1)]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"result":{"sync_info":{"latest_block_height":"%d"}}}`, h)
	}
}

func TestFetchBlockHeight(t *testing.T) {
	srv := httptest.NewServer(statusServer([]int64{42}))
	defer srv.Close()

	client := &http.Client{Timeout: time.Second}
	h, err := fetchBlockHeight(context.Background(), client, srv.URL+"/status")
	if err != nil {
		t.Fatalf("fetchBlockHeight: %v", err)
	}
	if h != 42 {
		t.Errorf("got height %d, want 42", h)
	}
}

func TestFetchBlockHeight_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: time.Second}
	_, err := fetchBlockHeight(context.Background(), client, srv.URL+"/status")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestFetchBlockHeight_invalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{not valid json}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: time.Second}
	_, err := fetchBlockHeight(context.Background(), client, srv.URL+"/status")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestFetchBlockHeight_cancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprintf(w, `{"result":{"sync_info":{"latest_block_height":"1"}}}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	client := &http.Client{Timeout: time.Second}
	_, err := fetchBlockHeight(ctx, client, srv.URL+"/status")
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

// TestWatchLoop_detectsReset verifies that a block height regression causes the
// chain connection to be replaced (the *Chain pointer in m.chains changes).
func TestWatchLoop_detectsReset(t *testing.T) {
	// Serve: height 5, height 5, then height 1 (regression → reset).
	srv := httptest.NewServer(statusServer([]int64{5, 5, 1}))
	defer srv.Close()

	gc := newTestGasCache(t)
	cfg := &config.ChainsConfig{
		Chains: []config.ChainConfig{standaloneChainCfg("devnet-1", "devnet", true)},
	}
	mgr, err := New(context.Background(), Options{
		Config:     cfg,
		GasCache:   gc,
		MnemonicFn: func() string { return testMnemonic },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer mgr.Close()
	mgr.Start(context.Background())

	original, _ := mgr.GetChain("devnet-1")

	orig := watcherPollInterval
	watcherPollInterval = 10 * time.Millisecond
	defer func() { watcherPollInterval = orig }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go mgr.watchLoop(ctx, "devnet-1", srv.URL)

	// Poll until the chain pointer changes or the context expires.
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out: chain was not reconnected after height regression")
		case <-time.After(20 * time.Millisecond):
			current, _ := mgr.GetChain("devnet-1")
			if current != original {
				return // reset was detected and chain was replaced
			}
		}
	}
}
