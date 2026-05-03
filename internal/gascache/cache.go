package gascache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// Reader is the read side of the cache; satisfies tx.GasCache.
type Reader interface {
	Lookup(ctx context.Context, chainID string) (*tx.CachedEstimate, bool)
}

// Writer is the write side; called by the HTTP handler after each broadcast.
type Writer interface {
	RecordSuccess(ctx context.Context, chainID string, gasUsed uint64, feeDenom, gasPriceAmount string) error
	RecordFailure(ctx context.Context, chainID string, reason string) error
}

// Cache implements both Reader and Writer backed by the chain_gas_cache SQLite table.
type Cache struct {
	db       *sql.DB
	interval time.Duration
}

// New creates a Cache backed by the given Store.
func New(s *store.Store) *Cache {
	return &Cache{db: s.DB(), interval: time.Hour}
}

// Lookup returns the cached estimate for chainID, or (nil, false) if not present.
// Implements tx.GasCache.
func (c *Cache) Lookup(ctx context.Context, chainID string) (*tx.CachedEstimate, bool) {
	var (
		baseGas        uint64
		gasPerOutput   uint64
		feeDenom       string
		gasPriceAmount string
		sampleCount    int
	)
	err := c.db.QueryRowContext(ctx, `
		SELECT base_gas, gas_per_output, fee_denom, gas_price_amount, sample_count
		FROM chain_gas_cache
		WHERE chain_id = ?
	`, chainID).Scan(&baseGas, &gasPerOutput, &feeDenom, &gasPriceAmount, &sampleCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false
		}
		return nil, false
	}
	return &tx.CachedEstimate{
		BaseGas:        baseGas,
		GasPerOutput:   gasPerOutput,
		FeeDenom:       feeDenom,
		GasPriceAmount: gasPriceAmount,
		SampleCount:    sampleCount,
	}, true
}

// RecordSuccess updates the cache with observed gas usage from a successful broadcast.
// Uses a cumulative moving average capped at a window of 20 samples.
func (c *Cache) RecordSuccess(ctx context.Context, chainID string, gasUsed uint64, feeDenom, gasPriceAmount string) error {
	now := time.Now().Unix()

	price, err := decimal.NewFromString(gasPriceAmount)
	if err != nil {
		return fmt.Errorf("gascache: invalid gas price %q: %w", gasPriceAmount, err)
	}
	normalizedPrice := price.String()

	_, err = c.db.ExecContext(ctx, `
		INSERT INTO chain_gas_cache
			(chain_id, base_gas, gas_per_output, fee_denom, gas_price_amount, sample_count, last_updated)
		VALUES (?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT(chain_id) DO UPDATE SET
			base_gas = (base_gas * MIN(sample_count, 20) + excluded.base_gas) / (MIN(sample_count, 20) + 1),
			fee_denom       = excluded.fee_denom,
			gas_price_amount = excluded.gas_price_amount,
			sample_count    = sample_count + 1,
			last_updated    = excluded.last_updated
	`, chainID, gasUsed, defaultGasPerOutput, feeDenom, normalizedPrice, now)
	if err != nil {
		return fmt.Errorf("gascache: record success for %s: %w", chainID, err)
	}
	return nil
}

const defaultGasPerOutput uint64 = 80_000

// RecordFailure records a broadcast failure reason without modifying gas values.
// reason is "out_of_gas" or "insufficient_fee".
func (c *Cache) RecordFailure(ctx context.Context, chainID, reason string) error {
	now := time.Now().Unix()
	_, err := c.db.ExecContext(ctx, `
		UPDATE chain_gas_cache
		SET last_failure_reason = ?, last_failure_at = ?
		WHERE chain_id = ?
	`, reason, now, chainID)
	if err != nil {
		return fmt.Errorf("gascache: record failure for %s: %w", chainID, err)
	}
	return nil
}
