package signed

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

const (
	PredicateNone       = "none"
	PredicateHasBalance = "has_balance"
)

var (
	ErrPredicateFailed   = errors.New("signed: predicate failed")
	ErrChainNotAvailable = errors.New("signed: predicate chain not available")
)

// BalanceQuerier queries token balance for an address on a chain.
type BalanceQuerier interface {
	QueryBalance(ctx context.Context, address, denom string) (tx.Coin, error)
}

// PredicateChecker evaluates configured predicates for signed-challenge requests.
type PredicateChecker struct {
	clients  map[string]BalanceQuerier
	prefixes map[string]string // chainID → bech32 prefix for address re-encoding
}

// NewPredicateChecker creates a PredicateChecker.
// clients maps chainID → querier; prefixes maps chainID → bech32 prefix.
func NewPredicateChecker(clients map[string]BalanceQuerier, prefixes map[string]string) *PredicateChecker {
	return &PredicateChecker{clients: clients, prefixes: prefixes}
}

// Check evaluates predicate for signerAddress on targetChainID.
// predicate is "none" or "has_balance". minAmount is a coin string (e.g. "1000000uatom"),
// required when predicate is "has_balance".
// Returns nil on pass, ErrPredicateFailed when the predicate condition is not met, and
// ErrChainNotAvailable when the target chain has no registered client.
func (c *PredicateChecker) Check(ctx context.Context, predicate, targetChainID, signerAddress, minAmount string) error {
	switch predicate {
	case PredicateNone, "":
		return nil
	case PredicateHasBalance:
		return c.checkBalance(ctx, targetChainID, signerAddress, minAmount)
	default:
		return fmt.Errorf("signed: unknown predicate %q", predicate)
	}
}

func (c *PredicateChecker) checkBalance(ctx context.Context, targetChainID, signerAddress, minAmount string) error {
	client, ok := c.clients[targetChainID]
	if !ok {
		return ErrChainNotAvailable
	}
	prefix, ok := c.prefixes[targetChainID]
	if !ok {
		return ErrChainNotAvailable
	}

	minCoin, err := config.ParseCoin(minAmount)
	if err != nil {
		return fmt.Errorf("signed: parse min_amount: %w", err)
	}

	targetAddr, err := reencodeAddress(signerAddress, prefix)
	if err != nil {
		return fmt.Errorf("signed: reencode address for %s: %w", targetChainID, err)
	}

	balance, err := client.QueryBalance(ctx, targetAddr, minCoin.Denom)
	if err != nil {
		return fmt.Errorf("signed: query balance on %s: %w", targetChainID, err)
	}

	got, ok1 := new(big.Int).SetString(balance.Amount, 10)
	want, ok2 := new(big.Int).SetString(minCoin.Amount, 10)
	if !ok1 || !ok2 {
		return fmt.Errorf("signed: parse amounts: balance=%q min=%q", balance.Amount, minCoin.Amount)
	}
	if got.Cmp(want) < 0 {
		return ErrPredicateFailed
	}
	return nil
}

// reencodeAddress decodes a bech32 address to raw bytes and re-encodes it under targetPrefix.
// This allows querying balance for the same key across chains with different bech32 prefixes.
func reencodeAddress(addr, targetPrefix string) (string, error) {
	raw, err := tx.DecodeAddressBytes(addr)
	if err != nil {
		return "", err
	}
	return tx.AddressFromBytes(raw, targetPrefix)
}
