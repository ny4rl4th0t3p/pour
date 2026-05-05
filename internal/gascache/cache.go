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

// Cache implements tx.GasCache backed by the chain_gas_cache SQLite table.
type Cache struct {
	db       *sql.DB
	interval time.Duration
}

// New creates a Cache backed by the given Store.
func New(s *store.Store) *Cache {
	return &Cache{db: s.DB(), interval: time.Hour}
}

// Lookup returns the cached estimate for (chainID, msgType), or (nil, false) if not present.
// msgType is MsgTypeSend ("send") or MsgTypeMultiSend ("multisend").
// Implements tx.GasCache.
func (c *Cache) Lookup(ctx context.Context, chainID, msgType string) (*tx.CachedEstimate, bool) {
	switch msgType {
	case tx.MsgTypeMultiSend:
		var gasPerOutput uint64
		var sampleCount int
		var feeDenom, gasPriceAmount string
		err := c.db.QueryRowContext(ctx, `
			SELECT multisend_gas_per_output, multisend_sample_count, fee_denom, gas_price_amount
			FROM chain_gas_cache
			WHERE chain_id = ? AND multisend_sample_count > 0
		`, chainID).Scan(&gasPerOutput, &sampleCount, &feeDenom, &gasPriceAmount)
		if err != nil {
			return nil, false
		}
		return &tx.CachedEstimate{
			BaseGas:        0,
			GasPerOutput:   gasPerOutput,
			FeeDenom:       feeDenom,
			GasPriceAmount: gasPriceAmount,
			SampleCount:    sampleCount,
		}, true

	default: // "send"
		var baseGas, gasPerOutput uint64
		var feeDenom, gasPriceAmount string
		var sampleCount int
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
}

// RecordSuccess updates the cache with observed gas usage from a successful broadcast.
// Uses a cumulative moving average capped at a window of 20 samples.
//
// For msgType="send": base_gas is the full observed gas; gas_per_output is stored as 0
// so that fee estimation uses base_gas alone.
// For msgType="multisend": multisend_gas_per_output tracks gasUsed/outputCount, converging
// to the marginal per-output cost for MsgMultiSend. Both are stored in the same row.
func (c *Cache) RecordSuccess(ctx context.Context, chainID, msgType string, gasUsed uint64, outputCount int, feeDenom, gasPriceAmount string) error {
	now := time.Now().Unix()

	price, err := decimal.NewFromString(gasPriceAmount)
	if err != nil {
		return fmt.Errorf("gascache: invalid gas price %q: %w", gasPriceAmount, err)
	}
	normalizedPrice := price.String()

	switch msgType {
	case tx.MsgTypeMultiSend:
		var gpo uint64
		if outputCount > 0 {
			gpo = gasUsed / uint64(outputCount)
		} else {
			gpo = gasUsed
		}
		_, err = c.db.ExecContext(ctx, `
			INSERT INTO chain_gas_cache
				(chain_id, base_gas, gas_per_output, fee_denom, gas_price_amount, sample_count,
				 last_updated, multisend_gas_per_output, multisend_sample_count)
			VALUES (?, 0, ?, ?, ?, 0, ?, ?, 1)
			ON CONFLICT(chain_id) DO UPDATE SET
				multisend_gas_per_output =
					(multisend_gas_per_output * MIN(multisend_sample_count, 20) + excluded.multisend_gas_per_output)
					/ (MIN(multisend_sample_count, 20) + 1),
				multisend_sample_count = multisend_sample_count + 1,
				fee_denom        = excluded.fee_denom,
				gas_price_amount = excluded.gas_price_amount,
				last_updated     = excluded.last_updated
		`, chainID, 0, feeDenom, normalizedPrice, now, gpo)

	default: // "send"
		_, err = c.db.ExecContext(ctx, `
			INSERT INTO chain_gas_cache
				(chain_id, base_gas, gas_per_output, fee_denom, gas_price_amount, sample_count, last_updated)
			VALUES (?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT(chain_id) DO UPDATE SET
				base_gas         = (base_gas * MIN(sample_count, 20) + excluded.base_gas) / (MIN(sample_count, 20) + 1),
				fee_denom        = excluded.fee_denom,
				gas_price_amount = excluded.gas_price_amount,
				sample_count     = sample_count + 1,
				last_updated     = excluded.last_updated
		`, chainID, gasUsed, 0, feeDenom, normalizedPrice, now)
	}
	if err != nil {
		return fmt.Errorf("gascache: record success for %s/%s: %w", chainID, msgType, err)
	}
	return nil
}

// RecordFailure records a broadcast failure reason without modifying gas values.
// reason is "out_of_gas", "insufficient_fee", or "broadcast_error".
func (c *Cache) RecordFailure(ctx context.Context, chainID, msgType, reason string) error {
	now := time.Now().Unix()
	_, err := c.db.ExecContext(ctx, `
		UPDATE chain_gas_cache
		SET last_failure_reason = ?, last_failure_at = ?
		WHERE chain_id = ?
	`, reason, now, chainID)
	if err != nil {
		return fmt.Errorf("gascache: record failure for %s/%s: %w", chainID, msgType, err)
	}
	return nil
}
