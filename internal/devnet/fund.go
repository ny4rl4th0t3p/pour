package devnet

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// fundingPollInterval is the delay between WaitForFunding balance checks.
// Exposed as a var so tests can override it without sleeping.
var fundingPollInterval = 2 * time.Second

// txFeeReserve is the amount (in the smallest unit of denom) kept in the funder
// account to cover transaction fees. For any realistic devnet gas price this is
// several orders of magnitude larger than the actual fee.
const txFeeReserve = "100000"

// BalanceQuerier can query a single address/denom balance. *tx.Client satisfies this.
type BalanceQuerier interface {
	QueryBalance(ctx context.Context, address, denom string) (tx.Coin, error)
}

// SelfFund creates a temporary tx.Client for funderMnemonic, checks whether
// pourAddr already holds denom, and if not broadcasts a bank send transferring
// the funder's full balance (minus a fee reserve) to pourAddr.
//
// funderMnemonic is used only inside tx.New for key derivation and is not
// retained after this function returns.
func SelfFund(ctx context.Context, info *chainregistry.ChainInfo, funderMnemonic, pourAddr, denom string) error {
	funder, err := tx.New(info, funderMnemonic, []uint32{0}, tx.Options{})
	if err != nil {
		return fmt.Errorf("devnet: create funder client: %w", err)
	}
	defer funder.Close()

	bal, err := funder.QueryBalance(ctx, pourAddr, denom)
	if err != nil {
		return fmt.Errorf("devnet: query pour balance: %w", err)
	}
	if bal.Amount != "0" {
		slog.InfoContext(ctx, "devnet: pour address already funded — skipping self-fund",
			"address", pourAddr, "balance", bal.Amount, "denom", denom)
		return nil
	}

	funderAddr, err := funder.AddressForKey(0)
	if err != nil {
		return fmt.Errorf("devnet: derive funder address: %w", err)
	}

	funderBal, err := funder.QueryBalance(ctx, funderAddr, denom)
	if err != nil {
		return fmt.Errorf("devnet: query funder balance: %w", err)
	}

	sendAmt, err := subtractReserve(funderBal.Amount, txFeeReserve)
	if err != nil {
		return fmt.Errorf("devnet: funder account %s has insufficient %s balance for self-fund (balance: %s): %w",
			funderAddr, denom, funderBal.Amount, err)
	}

	slog.InfoContext(ctx, "devnet: self-funding pour address",
		"from", funderAddr, "to", pourAddr, "amount", sendAmt, "denom", denom)

	if _, err = funder.BuildAndBroadcast(ctx, tx.SendRequest{
		KeyIndex:  0,
		ToAddress: pourAddr,
		Coins:     []tx.Coin{{Denom: denom, Amount: sendAmt}},
	}); err != nil {
		return fmt.Errorf("devnet: self-fund broadcast: %w", err)
	}

	slog.InfoContext(ctx, "devnet: self-funding complete", "address", pourAddr, "denom", denom)
	return nil
}

// WaitForFunding logs pourAddr and polls it every 2 s until a positive denom
// balance is detected or ctx is cancelled.
func WaitForFunding(ctx context.Context, querier BalanceQuerier, pourAddr, denom string) error {
	slog.InfoContext(ctx, "devnet: pour address has no funds — send tokens to start serving requests",
		"address", pourAddr, "denom", denom)

	for {
		bal, err := querier.QueryBalance(ctx, pourAddr, denom)
		if err == nil && bal.Amount != "0" {
			slog.InfoContext(ctx, "devnet: funding detected — starting server",
				"address", pourAddr, "balance", bal.Amount, "denom", denom)
			return nil
		}
		if !devnetSleep(ctx, fundingPollInterval) {
			return ctx.Err()
		}
	}
}

// subtractReserve returns balance - reserve as a decimal string, or an error if
// the result would be <= 0.
func subtractReserve(balance, reserve string) (string, error) {
	b, ok := new(big.Int).SetString(balance, 10)
	if !ok {
		return "", fmt.Errorf("invalid balance %q", balance)
	}
	r, ok := new(big.Int).SetString(reserve, 10)
	if !ok {
		return "", fmt.Errorf("invalid reserve %q", reserve)
	}
	result := new(big.Int).Sub(b, r)
	if result.Sign() <= 0 {
		return "", fmt.Errorf("balance %s is not enough to cover reserve %s", balance, reserve)
	}
	return result.String(), nil
}

func devnetSleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}
