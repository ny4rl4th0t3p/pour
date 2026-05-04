package tx

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/shopspring/decimal"

	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

func newTestClient(t *testing.T, conn *grpc.ClientConn, chain *chainregistry.ChainInfo) *Client {
	t.Helper()
	return &Client{
		chain:   chain,
		conn:    conn,
		txSvc:   txv1beta1.NewServiceClient(conn),
		authSvc: authv1beta1.NewQueryClient(conn),
		opts:    Options{},
	}
}

func TestBuildAndBroadcast_happyPath(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	conn := fakechain.Start(t, fakechain.Config{
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
		Mnemonic:  testMnemonic,
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

func TestBuildAndBroadcast_accountNotFound(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{
		// Address deliberately does not match what the mnemonic derives.
		Address: "osmo1differentaddressthatwontmatch000000000",
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		Slip44:       118,
	}

	_, err := newTestClient(t, conn, chain).BuildAndBroadcast(t.Context(), SendRequest{
		Mnemonic:  testMnemonic,
		KeyIndex:  0,
		ToAddress: testToAddr,
		Coins:     Coins{{Denom: "uosmo", Amount: "1000000"}},
	})
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}
