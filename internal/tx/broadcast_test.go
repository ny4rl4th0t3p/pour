package tx

import (
	"errors"
	"testing"
	"time"

	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
)

const testTxHash = "AABBCCDDEEFF00112233445566778899AABBCCDDEEFF00112233445566778899"

func emptyTxRaw() *txv1beta1.TxRaw {
	return &txv1beta1.TxRaw{BodyBytes: []byte("body"), AuthInfoBytes: []byte("auth")}
}

func TestBroadcast_success(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{BroadcastTxHash: testTxHash})
	svc := txv1beta1.NewServiceClient(conn)

	hash, err := broadcast(t.Context(), svc, emptyTxRaw())
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
	svc := txv1beta1.NewServiceClient(conn)

	_, err := broadcast(t.Context(), svc, emptyTxRaw())
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
	svc := txv1beta1.NewServiceClient(conn)

	result, err := waitForConfirmation(t.Context(), svc, testTxHash)
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
	// Speed up polling so the test doesn't take 4 seconds.
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	conn := fakechain.Start(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		TxHeight:        200,
		ConfirmAfter:    2, // first 2 calls return NotFound
	})
	svc := txv1beta1.NewServiceClient(conn)

	result, err := waitForConfirmation(t.Context(), svc, testTxHash)
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
	svc := txv1beta1.NewServiceClient(conn)

	_, err := waitForConfirmation(t.Context(), svc, testTxHash)
	if !errors.Is(err, ErrBroadcastTimeout) {
		t.Errorf("expected ErrBroadcastTimeout, got %v", err)
	}
}
