package devnet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// mockQuerier is a BalanceQuerier that returns pre-configured responses in order.
type mockQuerier struct {
	responses []mockBalanceResponse
	calls     int
}

type mockBalanceResponse struct {
	coin tx.Coin
	err  error
}

func (m *mockQuerier) QueryBalance(_ context.Context, _, _ string) (tx.Coin, error) {
	if m.calls >= len(m.responses) {
		return tx.Coin{Amount: "0"}, nil
	}
	r := m.responses[m.calls]
	m.calls++
	return r.coin, r.err
}

func TestSubtractReserve(t *testing.T) {
	tests := []struct {
		name    string
		balance string
		reserve string
		want    string
		wantErr bool
	}{
		{"normal", "1000000", "100000", "900000", false},
		{"exact", "100001", "100000", "1", false},
		{"equal", "100000", "100000", "", true},
		{"negative", "50000", "100000", "", true},
		{"zero balance", "0", "100000", "", true},
		{"invalid balance", "notanumber", "100000", "", true},
		{"invalid reserve", "1000000", "notanumber", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := subtractReserve(tc.balance, tc.reserve)
			if (err != nil) != tc.wantErr {
				t.Fatalf("subtractReserve(%q, %q): error = %v, wantErr = %v", tc.balance, tc.reserve, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("subtractReserve(%q, %q): got %q, want %q", tc.balance, tc.reserve, got, tc.want)
			}
		})
	}
}

func TestWaitForFunding_receivesBalance(t *testing.T) {
	q := &mockQuerier{responses: []mockBalanceResponse{
		{coin: tx.Coin{Denom: "uatom", Amount: "0"}},
		{coin: tx.Coin{Denom: "uatom", Amount: "0"}},
		{coin: tx.Coin{Denom: "uatom", Amount: "1000000"}},
	}}

	// Override poll interval for speed.
	origInterval := fundingPollInterval
	fundingPollInterval = time.Millisecond
	defer func() { fundingPollInterval = origInterval }()

	ctx := context.Background()
	if err := WaitForFunding(ctx, q, "cosmos1abc", "uatom"); err != nil {
		t.Fatalf("WaitForFunding: unexpected error: %v", err)
	}
	if q.calls != 3 {
		t.Errorf("expected 3 QueryBalance calls, got %d", q.calls)
	}
}

func TestWaitForFunding_cancelledContext(t *testing.T) {
	q := &mockQuerier{responses: []mockBalanceResponse{
		{coin: tx.Coin{Denom: "uatom", Amount: "0"}},
	}}

	origInterval := fundingPollInterval
	fundingPollInterval = 50 * time.Millisecond
	defer func() { fundingPollInterval = origInterval }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := WaitForFunding(ctx, q, "cosmos1abc", "uatom")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestWaitForFunding_queryErrorRetried(t *testing.T) {
	q := &mockQuerier{responses: []mockBalanceResponse{
		{err: errors.New("connection refused")},
		{coin: tx.Coin{Denom: "uatom", Amount: "500000"}},
	}}

	origInterval := fundingPollInterval
	fundingPollInterval = time.Millisecond
	defer func() { fundingPollInterval = origInterval }()

	ctx := context.Background()
	if err := WaitForFunding(ctx, q, "cosmos1abc", "uatom"); err != nil {
		t.Fatalf("WaitForFunding: unexpected error: %v", err)
	}
}
