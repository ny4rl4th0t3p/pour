package ratelimit

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/store"
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

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ratelimit: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // superseded by explicit Commit on success path

	now := time.Now().Unix()
	windowStart := now - int64(l.window/time.Second)

	var total int64
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(request_count), 0)
		FROM rate_limits
		WHERE scope_type = 'ip' AND scope_value = ? AND chain_id = ? AND window_start >= ?
	`, ip, chainID, windowStart).Scan(&total)
	if err != nil {
		return fmt.Errorf("ratelimit: count: %w", err)
	}

	if total >= int64(l.requestsPerWindow) {
		var oldest int64
		err = tx.QueryRowContext(ctx, `
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

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rate_limits (scope_type, scope_value, chain_id, window_start, request_count, coins_total)
		VALUES ('ip', ?, ?, ?, 1, '')
		ON CONFLICT(scope_type, scope_value, chain_id, window_start)
		DO UPDATE SET request_count = request_count + 1
	`, ip, chainID, now); err != nil {
		return fmt.Errorf("ratelimit: insert: %w", err)
	}

	return tx.Commit()
}
