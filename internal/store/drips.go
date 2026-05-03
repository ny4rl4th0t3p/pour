package store

import (
	"context"
	"fmt"
)

// DripRecord holds all fields written to the drips table on a successful pour.
type DripRecord struct {
	ChainID     string
	Address     string
	Coins       string // e.g. "1000000uosmo"
	RequesterIP string
	TxHash      string
	Status      string // "confirmed"
	RequestedAt int64  // Unix seconds
	CompletedAt int64  // Unix seconds
}

// RecordDrip inserts a confirmed drip into the audit log and returns the new row id.
func (s *Store) RecordDrip(ctx context.Context, d DripRecord) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO drips
			(chain_id, address, coins, requester_ip, tx_hash, status, requested_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, d.ChainID, d.Address, d.Coins, d.RequesterIP, d.TxHash, d.Status, d.RequestedAt, d.CompletedAt)
	if err != nil {
		return 0, fmt.Errorf("store: record drip: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: last insert id: %w", err)
	}
	return id, nil
}
