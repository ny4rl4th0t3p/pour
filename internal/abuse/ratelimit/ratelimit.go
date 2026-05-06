package ratelimit

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// DefaultRequestsPerWindow is the per-IP request ceiling per window when no
// explicit value is set in chains.yml.
const DefaultRequestsPerWindow = 10

// ErrRateLimitExceeded is returned by Check when the caller has exhausted their
// sliding-window allowance. RetryAfter is how long until the oldest request in
// the current window expires and the count drops below the limit.
type ErrRateLimitExceeded struct {
	RetryAfter time.Duration
}

func (e *ErrRateLimitExceeded) Error() string {
	return fmt.Sprintf("rate limit exceeded; retry after %s", e.RetryAfter.Round(time.Second))
}

// Limiter implements an IP-based sliding-window rate limiter backed by SQLite.
type Limiter struct {
	mu                sync.Mutex
	db                *sql.DB
	requestsPerWindow int
	window            time.Duration
}

// New creates a Limiter. requestsPerWindow is the per-IP ceiling within window.
func New(s *store.Store, requestsPerWindow int, window time.Duration) *Limiter {
	return &Limiter{
		db:                s.DB(),
		requestsPerWindow: requestsPerWindow,
		window:            window,
	}
}

// Check verifies that ip has not exceeded the hourly request limit for chainID.
// If under the limit, the request is recorded and nil is returned.
// If over the limit, *ErrRateLimitExceeded is returned and nothing is recorded.
func (l *Limiter) Check(ctx context.Context, ip, chainID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	txn, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ratelimit: begin tx: %w", err)
	}
	defer txn.Rollback() //nolint:errcheck // superseded by explicit Commit on success path

	now := time.Now().Unix()
	windowStart := now - int64(l.window/time.Second)

	var total int64
	err = txn.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(request_count), 0)
		FROM rate_limits
		WHERE scope_type = 'ip' AND scope_value = ? AND chain_id = ? AND window_start >= ?
	`, ip, chainID, windowStart).Scan(&total)
	if err != nil {
		return fmt.Errorf("ratelimit: count: %w", err)
	}

	if total >= int64(l.requestsPerWindow) {
		var oldest int64
		err = txn.QueryRowContext(ctx, `
			SELECT MIN(window_start)
			FROM rate_limits
			WHERE scope_type = 'ip' AND scope_value = ? AND chain_id = ? AND window_start >= ?
		`, ip, chainID, windowStart).Scan(&oldest)
		if err != nil {
			return fmt.Errorf("ratelimit: oldest: %w", err)
		}
		retryAfter := time.Until(time.Unix(oldest, 0).Add(l.window))
		return &ErrRateLimitExceeded{RetryAfter: retryAfter}
	}

	if _, err := txn.ExecContext(ctx, `
		INSERT INTO rate_limits (scope_type, scope_value, chain_id, window_start, request_count, coins_total)
		VALUES ('ip', ?, ?, ?, 1, '')
		ON CONFLICT(scope_type, scope_value, chain_id, window_start)
		DO UPDATE SET request_count = request_count + 1
	`, ip, chainID, now); err != nil {
		return fmt.Errorf("ratelimit: insert: %w", err)
	}

	return txn.Commit()
}

const addressWindow = 24 * time.Hour

// CheckAddress verifies that bech32Addr has not exceeded its per-address coin drip
// cap for chainID within a rolling 24-hour window. keyAlgo (e.g. "secp256k1") is
// included in the scope key so that the same raw address bytes shared across chain
// prefixes count against the same bucket. If the check passes, dripCoin is recorded.
func (l *Limiter) CheckAddress(
	ctx context.Context,
	bech32Addr, keyAlgo, chainID string,
	dripCoin, maxPerDay tx.Coin,
) error {
	rawBytes, err := tx.DecodeAddressBytes(bech32Addr)
	if err != nil {
		return fmt.Errorf("ratelimit: decode address: %w", err)
	}
	scopeValue := keyAlgo + ":" + hex.EncodeToString(rawBytes)

	dripAmt, ok := new(big.Int).SetString(dripCoin.Amount, 10)
	if !ok {
		return fmt.Errorf("ratelimit: invalid drip amount: %q", dripCoin.Amount)
	}
	maxAmt, ok := new(big.Int).SetString(maxPerDay.Amount, 10)
	if !ok {
		return fmt.Errorf("ratelimit: invalid max amount: %q", maxPerDay.Amount)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	txn, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ratelimit: begin tx: %w", err)
	}
	defer txn.Rollback() //nolint:errcheck // superseded by explicit Commit on success path

	now := time.Now().Unix()
	windowStart := now - int64(addressWindow/time.Second)

	var totalCoins int64
	err = txn.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CAST(coins_total AS INTEGER)), 0)
		FROM rate_limits
		WHERE scope_type = 'address' AND scope_value = ? AND chain_id = ? AND window_start >= ?
	`, scopeValue, chainID, windowStart).Scan(&totalCoins)
	if err != nil {
		return fmt.Errorf("ratelimit: sum coins: %w", err)
	}

	total := big.NewInt(totalCoins)
	if new(big.Int).Add(total, dripAmt).Cmp(maxAmt) > 0 {
		var oldest int64
		err = txn.QueryRowContext(ctx, `
			SELECT COALESCE(MIN(window_start), ?)
			FROM rate_limits
			WHERE scope_type = 'address' AND scope_value = ? AND chain_id = ? AND window_start >= ?
		`, now, scopeValue, chainID, windowStart).Scan(&oldest)
		if err != nil {
			return fmt.Errorf("ratelimit: oldest: %w", err)
		}
		return &ErrRateLimitExceeded{RetryAfter: time.Until(time.Unix(oldest, 0).Add(addressWindow))}
	}

	if _, err := txn.ExecContext(ctx, `
		INSERT INTO rate_limits (scope_type, scope_value, chain_id, window_start, request_count, coins_total)
		VALUES ('address', ?, ?, ?, 1, ?)
		ON CONFLICT(scope_type, scope_value, chain_id, window_start)
		DO UPDATE SET request_count  = request_count + 1,
		              coins_total     = CAST(CAST(coins_total AS INTEGER) + ? AS TEXT)
	`, scopeValue, chainID, now, dripCoin.Amount, dripCoin.Amount); err != nil {
		return fmt.Errorf("ratelimit: insert: %w", err)
	}

	return txn.Commit()
}

const apiKeyWindow = time.Hour

// CheckAPIKey verifies that keyID has not exceeded its per-key request ceiling for
// chainID within a rolling 1-hour window. rateLimitPerHour overrides the Limiter's
// default requestsPerWindow when > 0 (useful for per-key custom limits).
// If the check passes, the request is recorded.
func (l *Limiter) CheckAPIKey(ctx context.Context, keyID, chainID string, rateLimitPerHour int) error {
	limit := l.requestsPerWindow
	if rateLimitPerHour > 0 {
		limit = rateLimitPerHour
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	txn, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ratelimit: begin tx: %w", err)
	}
	defer txn.Rollback() //nolint:errcheck // superseded by explicit Commit on success path

	now := time.Now().Unix()
	windowStart := now - int64(apiKeyWindow/time.Second)

	var total int64
	err = txn.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(request_count), 0)
		FROM rate_limits
		WHERE scope_type = 'api_key' AND scope_value = ? AND chain_id = ? AND window_start >= ?
	`, keyID, chainID, windowStart).Scan(&total)
	if err != nil {
		return fmt.Errorf("ratelimit: count: %w", err)
	}

	if total >= int64(limit) {
		var oldest int64
		err = txn.QueryRowContext(ctx, `
			SELECT COALESCE(MIN(window_start), ?)
			FROM rate_limits
			WHERE scope_type = 'api_key' AND scope_value = ? AND chain_id = ? AND window_start >= ?
		`, now, keyID, chainID, windowStart).Scan(&oldest)
		if err != nil {
			return fmt.Errorf("ratelimit: oldest: %w", err)
		}
		return &ErrRateLimitExceeded{RetryAfter: time.Until(time.Unix(oldest, 0).Add(apiKeyWindow))}
	}

	if _, err := txn.ExecContext(ctx, `
		INSERT INTO rate_limits (scope_type, scope_value, chain_id, window_start, request_count, coins_total)
		VALUES ('api_key', ?, ?, ?, 1, '')
		ON CONFLICT(scope_type, scope_value, chain_id, window_start)
		DO UPDATE SET request_count = request_count + 1
	`, keyID, chainID, now); err != nil {
		return fmt.Errorf("ratelimit: insert: %w", err)
	}

	return txn.Commit()
}
