package tx

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/shopspring/decimal"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

func newTestClient(t *testing.T, conn *grpc.ClientConn, chain *chainregistry.ChainInfo) *Client {
	t.Helper()
	privKey, err := keys.DerivePrivKey(testMnemonic, chain.Slip44, 0)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	return &Client{
		chain: chain,
		active: &grpcTransport{
			url:     "test",
			conn:    conn,
			txSvc:   txv1beta1.NewServiceClient(conn),
			authSvc: authv1beta1.NewQueryClient(conn),
		},
		cachedKeys: map[uint32]*keys.PrivKey{0: privKey},
		opts:       Options{},
	}
}

func TestBuildAndBroadcast_happyPath(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	conn := fakechain.StartGRPC(t, fakechain.Config{
		Address:         testFromAddr,
		AccountNumber:   1,
		Sequence:        0,
		GasUsed:         100_000,
		BroadcastTxHash: testTxHash,
		TxHeight:        50,
		ConfirmAfter:    0,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	result, err := newTestClient(t, conn, chain).BuildAndBroadcast(t.Context(), SendRequest{
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcast: %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
	if result.Height != 50 {
		t.Errorf("Height: got %d, want 50", result.Height)
	}
}

func TestBuildAndBroadcastMulti_happyPath(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	conn := fakechain.StartGRPC(t, fakechain.Config{
		Address:         testFromAddr,
		AccountNumber:   1,
		Sequence:        0,
		GasUsed:         200_000,
		BroadcastTxHash: testTxHash,
		TxHeight:        51,
		GetTxGasUsed:    180_000,
		ConfirmAfter:    0,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	result, err := newTestClient(t, conn, chain).BuildAndBroadcastMulti(t.Context(), BatchSendRequest{
		KeyIndex: 0,
		Outputs: []SendOutput{
			{ToAddress: testToAddr, Coins: Coins{{Denom: "uosmo", Amount: "500000"}}},
			{ToAddress: testFromAddr, Coins: Coins{{Denom: "uosmo", Amount: "500000"}}},
		},
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcastMulti: %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
	if result.Height != 51 {
		t.Errorf("Height: got %d, want 51", result.Height)
	}
	if result.GasUsed != 180_000 {
		t.Errorf("GasUsed: got %d, want 180000", result.GasUsed)
	}
}

func TestBuildAndBroadcastMulti_emptyOutputs(t *testing.T) {
	conn := fakechain.StartGRPC(t, fakechain.Config{})
	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
	}

	_, err := newTestClient(t, conn, chain).BuildAndBroadcastMulti(t.Context(), BatchSendRequest{
		KeyIndex: 0,
		Outputs:  nil,
	})
	if err == nil {
		t.Fatal("expected error for empty outputs")
	}
}

func TestBuildAndBroadcast_sequenceMismatchRetry(t *testing.T) {
	origInterval := confirmPollInterval
	origDelay := sequenceRetryDelay
	confirmPollInterval = 5 * time.Millisecond
	sequenceRetryDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		confirmPollInterval = origInterval
		sequenceRetryDelay = origDelay
	})

	// First broadcast returns ABCI code 32 (wrong sequence); second succeeds.
	// First account query returns seq 0; second returns seq 1 (as if refill tx committed).
	conn := fakechain.StartGRPC(t, fakechain.Config{
		Address:           testFromAddr,
		AccountNumber:     1,
		SequencesPerQuery: []uint64{0, 1},
		GasUsed:           100_000,
		BroadcastTxHash:   testTxHash,
		BroadcastCodes:    []uint32{32, 0},
		TxHeight:          50,
		ConfirmAfter:      0,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	result, err := newTestClient(t, conn, chain).BuildAndBroadcast(t.Context(), SendRequest{
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcast: expected retry to succeed, got %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
}

func TestBuildAndBroadcast_accountNotFound(t *testing.T) {
	conn := fakechain.StartGRPC(t, fakechain.Config{
		// Address deliberately does not match what the mnemonic derives.
		Address: "osmo1differentaddressthatwontmatch000000000",
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
	}

	_, err := newTestClient(t, conn, chain).BuildAndBroadcast(t.Context(), SendRequest{
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestBuildAndBroadcast_RESTOnly(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	baseURL := fakechain.StartREST(t, fakechain.Config{
		Address:         testFromAddr,
		AccountNumber:   1,
		Sequence:        0,
		GasUsed:         100_000,
		BroadcastTxHash: testTxHash,
		TxHeight:        55,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}
	privKey, err := keys.DerivePrivKey(testMnemonic, chain.Slip44, 0)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	c := &Client{
		chain:      chain,
		active:     newRESTTransport(baseURL),
		usingREST:  true,
		cachedKeys: map[uint32]*keys.PrivKey{0: privKey},
		opts:       Options{},
	}

	result, err := c.BuildAndBroadcast(t.Context(), SendRequest{
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcast: %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
	if result.Height != 55 {
		t.Errorf("Height: got %d, want 55", result.Height)
	}
}

func TestBuildAndBroadcast_GRPCtoRESTFailover(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	// gRPC server that always returns codes.Unavailable for all calls.
	grpcConn := fakechain.StartGRPC(t, fakechain.Config{Unavailable: true})

	// REST server that handles the request successfully after failover.
	restURL := fakechain.StartREST(t, fakechain.Config{
		Address:         testFromAddr,
		AccountNumber:   1,
		Sequence:        0,
		GasUsed:         100_000,
		BroadcastTxHash: testTxHash,
		TxHeight:        60,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}
	privKey, err := keys.DerivePrivKey(testMnemonic, chain.Slip44, 0)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	// The grpcTransportFrom helper sets url = "test"; the pool must use the same sentinel.
	c := &Client{
		chain:      chain,
		active:     grpcTransportFrom(grpcConn),
		grpcPool:   NewEndpointPool([]string{"test"}),
		restPool:   NewEndpointPool([]string{restURL}),
		usingREST:  false,
		cachedKeys: map[uint32]*keys.PrivKey{0: privKey},
		opts:       Options{},
	}

	result, err := c.BuildAndBroadcast(t.Context(), SendRequest{
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcast: %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
	if result.Height != 60 {
		t.Errorf("Height: got %d, want 60", result.Height)
	}
}

func TestBuildAndBroadcast_RESTSequenceMismatch(t *testing.T) {
	origInterval := confirmPollInterval
	origDelay := sequenceRetryDelay
	confirmPollInterval = 5 * time.Millisecond
	sequenceRetryDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		confirmPollInterval = origInterval
		sequenceRetryDelay = origDelay
	})

	// First broadcast returns ABCI code 32 (sequence mismatch); second succeeds.
	// First account query returns seq 0; second returns seq 1 (after re-query).
	baseURL := fakechain.StartREST(t, fakechain.Config{
		Address:           testFromAddr,
		AccountNumber:     1,
		SequencesPerQuery: []uint64{0, 1},
		GasUsed:           100_000,
		BroadcastTxHash:   testTxHash,
		BroadcastCodes:    []uint32{32, 0},
		TxHeight:          65,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uosmo", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}
	privKey, err := keys.DerivePrivKey(testMnemonic, chain.Slip44, 0)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}

	c := &Client{
		chain:      chain,
		active:     newRESTTransport(baseURL),
		usingREST:  true,
		cachedKeys: map[uint32]*keys.PrivKey{0: privKey},
		opts:       Options{},
	}

	result, err := c.BuildAndBroadcast(t.Context(), SendRequest{
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcast: expected retry to succeed, got %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
}
