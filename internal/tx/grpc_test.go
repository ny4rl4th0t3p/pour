package tx

import (
	"errors"
	"testing"
	"time"

	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"

	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	bankv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/bank/v1beta1"

	"google.golang.org/grpc"
)

const testTxHash = "AABBCCDDEEFF00112233445566778899AABBCCDDEEFF00112233445566778899"

func emptyTxRaw() *txv1beta1.TxRaw {
	return &txv1beta1.TxRaw{BodyBytes: []byte("body"), AuthInfoBytes: []byte("auth")}
}

func grpcTransportFrom(conn *grpc.ClientConn) *grpcTransport {
	return &grpcTransport{
		url:     "test",
		conn:    conn,
		txSvc:   txv1beta1.NewServiceClient(conn),
		authSvc: authv1beta1.NewQueryClient(conn),
		bankSvc: bankv1beta1.NewQueryClient(conn),
	}
}

func TestBroadcast_success(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{BroadcastTxHash: testTxHash})
	gt := grpcTransportFrom(conn)

	hash, err := broadcast(t.Context(), gt, emptyTxRaw())
	if err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if hash != testTxHash {
		t.Errorf("hash: got %s, want %s", hash, testTxHash)
	}
}

func TestBroadcast_abciError(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		BroadcastCode:   5, // insufficient funds
	})
	gt := grpcTransportFrom(conn)

	_, err := broadcast(t.Context(), gt, emptyTxRaw())
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestWaitForConfirmation_immediate(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		TxHeight:        100,
		ConfirmAfter:    0,
	})
	gt := grpcTransportFrom(conn)

	result, err := gt.waitForConfirmation(t.Context(), testTxHash)
	if err != nil {
		t.Fatalf("waitForConfirmation: %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash: got %s, want %s", result.TxHash, testTxHash)
	}
	if result.Height != 100 {
		t.Errorf("Height: got %d, want 100", result.Height)
	}
}

func TestWaitForConfirmation_afterPolling(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	conn := fakechain.Start(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		TxHeight:        200,
		ConfirmAfter:    2,
	})
	gt := grpcTransportFrom(conn)

	result, err := gt.waitForConfirmation(t.Context(), testTxHash)
	if err != nil {
		t.Fatalf("waitForConfirmation: %v", err)
	}
	if result.Height != 200 {
		t.Errorf("Height: got %d, want 200", result.Height)
	}
}

func TestWaitForConfirmation_timeout(t *testing.T) {
	origTimeout := confirmTimeout
	origInterval := confirmPollInterval
	confirmTimeout = 20 * time.Millisecond
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		confirmTimeout = origTimeout
		confirmPollInterval = origInterval
	})

	conn := fakechain.Start(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		ConfirmAfter:    9999,
	})
	gt := grpcTransportFrom(conn)

	_, err := gt.waitForConfirmation(t.Context(), testTxHash)
	if !errors.Is(err, ErrBroadcastTimeout) {
		t.Errorf("expected ErrBroadcastTimeout, got %v", err)
	}
}
