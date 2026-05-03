package gascache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"
)

// LowGasPriceFn returns the configured low gas price floor for a chain, if any.
type LowGasPriceFn func(chainID string) (amount string, ok bool)

// Start launches the decay goroutine. It ticks every c.interval and stops when ctx is canceled.
func (c *Cache) Start(ctx context.Context, lowGasPrice LowGasPriceFn) {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.runDecay(ctx, lowGasPrice); err != nil {
					slog.Error("gascache: decay error", slog.Any("err", err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

var (
	decayMultiplierWithFloor    = decimal.NewFromFloat(0.9)
	decayMultiplierWithoutFloor = decimal.NewFromFloat(0.95)
)

// runDecay iterates all cached chains and decays gas prices toward their floors.
func (c *Cache) runDecay(ctx context.Context, lowGasPrice LowGasPriceFn) error {
	rows, err := c.db.QueryContext(ctx, `SELECT chain_id, gas_price_amount FROM chain_gas_cache`)
	if err != nil {
		return fmt.Errorf("gascache: decay query: %w", err)
	}
	defer rows.Close()

	now := time.Now().Unix()

	for rows.Next() {
		var chainID, gasPriceAmount string
		if err := rows.Scan(&chainID, &gasPriceAmount); err != nil {
			return fmt.Errorf("gascache: decay scan: %w", err)
		}

		current, err := decimal.NewFromString(gasPriceAmount)
		if err != nil {
			continue // skip malformed rows
		}

		var target decimal.Decimal
		if floor, ok := lowGasPrice(chainID); ok {
			floorDec, err := decimal.NewFromString(floor)
			if err == nil {
				candidate := current.Mul(decayMultiplierWithFloor)
				if candidate.LessThan(floorDec) {
					target = floorDec
				} else {
					target = candidate
				}
			} else {
				target = current.Mul(decayMultiplierWithoutFloor)
			}
		} else {
			target = current.Mul(decayMultiplierWithoutFloor)
		}

		if target.GreaterThanOrEqual(current) {
			continue
		}

		if _, err := c.db.ExecContext(ctx, `
			UPDATE chain_gas_cache
			SET gas_price_amount = ?, last_decay_at = ?
			WHERE chain_id = ?
		`, target.String(), now, chainID); err != nil {
			return fmt.Errorf("gascache: decay update %s: %w", chainID, err)
		}
	}

	return rows.Err()
}
