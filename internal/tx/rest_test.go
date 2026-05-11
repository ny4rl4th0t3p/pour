package tx

import (
	"errors"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
)

func restTransportFor(t *testing.T, cfg fakechain.Config) *restTransport {
	t.Helper()
	return newRESTTransport(fakechain.StartREST(t, cfg))
}

// ---- queryAccount ----

func TestRESTQueryAccount_success(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{
		Address:       testFromAddr,
		AccountNumber: 42,
		Sequence:      7,
	})
	acc, err := rt.queryAccount(t.Context(), testFromAddr)
	if err != nil {
		t.Fatalf("queryAccount: %v", err)
	}
	if acc.AccountNumber != 42 {
		t.Errorf("AccountNumber: got %d, want 42", acc.AccountNumber)
	}
	if acc.Sequence != 7 {
		t.Errorf("Sequence: got %d, want 7", acc.Sequence)
	}
}

func TestRESTQueryAccount_notFound(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{Address: testFromAddr})
	_, err := rt.queryAccount(t.Context(), "cosmos1differentaddress000000000000000000000")
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

// ---- simulate ----

func TestRESTSimulate_success(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{GasUsed: 80_000})
	gas, err := rt.simulate(t.Context(), []byte("placeholder"))
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if gas != 80_000 {
		t.Errorf("GasUsed: got %d, want 80000", gas)
	}
}

func TestRESTSimulate_unavailableReturnsZero(t *testing.T) {
	// GasUsed == 0 → fake server returns 501; simulate must return (0, nil).
	rt := restTransportFor(t, fakechain.Config{GasUsed: 0})
	gas, err := rt.simulate(t.Context(), []byte("placeholder"))
	if err != nil {
		t.Fatalf("simulate: unexpected error: %v", err)
	}
	if gas != 0 {
		t.Errorf("GasUsed: got %d, want 0", gas)
	}
}

// ---- broadcastTx ----

func TestRESTBroadcastTx_success(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{BroadcastTxHash: testTxHash})
	hash, err := rt.broadcastTx(t.Context(), []byte("txbytes"))
	if err != nil {
		t.Fatalf("broadcastTx: %v", err)
	}
	if hash != testTxHash {
		t.Errorf("hash: got %s, want %s", hash, testTxHash)
	}
}

func TestRESTBroadcastTx_abciError(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		BroadcastCode:   5, // insufficient funds
	})
	_, err := rt.broadcastTx(t.Context(), []byte("txbytes"))
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestRESTBroadcastTx_unavailable(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{Unavailable: true})
	_, err := rt.broadcastTx(t.Context(), []byte("txbytes"))
	if !errors.Is(err, errRESTUnavailable) {
		t.Errorf("expected errRESTUnavailable, got %v", err)
	}
}

// ---- waitForConfirmation ----

func TestRESTWaitForConfirmation_immediate(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		TxHeight:        100,
		ConfirmAfter:    0,
	})
	result, err := rt.waitForConfirmation(t.Context(), testTxHash)
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

func TestRESTWaitForConfirmation_afterPolling(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	rt := restTransportFor(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		TxHeight:        200,
		ConfirmAfter:    2,
	})
	result, err := rt.waitForConfirmation(t.Context(), testTxHash)
	if err != nil {
		t.Fatalf("waitForConfirmation: %v", err)
	}
	if result.Height != 200 {
		t.Errorf("Height: got %d, want 200", result.Height)
	}
}

func TestRESTWaitForConfirmation_timeout(t *testing.T) {
	origTimeout := confirmTimeout
	origInterval := confirmPollInterval
	confirmTimeout = 20 * time.Millisecond
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		confirmTimeout = origTimeout
		confirmPollInterval = origInterval
	})

	rt := restTransportFor(t, fakechain.Config{
		BroadcastTxHash: testTxHash,
		ConfirmAfter:    9999,
	})
	_, err := rt.waitForConfirmation(t.Context(), testTxHash)
	if !errors.Is(err, ErrBroadcastTimeout) {
		t.Errorf("expected ErrBroadcastTimeout, got %v", err)
	}
}

// ---- queryBalance ----

func TestRESTQueryBalance_success(t *testing.T) {
	rt := restTransportFor(t, fakechain.Config{BalanceAmount: "5000000"})
	coin, err := rt.queryBalance(t.Context(), testFromAddr, "uosmo")
	if err != nil {
		t.Fatalf("queryBalance: %v", err)
	}
	if coin.Amount != "5000000" {
		t.Errorf("Amount: got %s, want 5000000", coin.Amount)
	}
	if coin.Denom != "uosmo" {
		t.Errorf("Denom: got %s, want uosmo", coin.Denom)
	}
}

func TestRESTQueryBalance_empty(t *testing.T) {
	// BalanceAmount == "" → fake server returns "0".
	rt := restTransportFor(t, fakechain.Config{})
	coin, err := rt.queryBalance(t.Context(), testFromAddr, "uosmo")
	if err != nil {
		t.Fatalf("queryBalance: %v", err)
	}
	if coin.Amount != "0" {
		t.Errorf("Amount: got %s, want 0", coin.Amount)
	}
}
