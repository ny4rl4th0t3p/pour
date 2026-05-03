package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordDrip(t *testing.T) {
	s, err := New(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	now := time.Now().Unix()
	rec := DripRecord{
		ChainID:     "osmosis-1",
		Address:     "osmo1abc123",
		Coins:       "1000000uosmo",
		Tier:        "anonymous_rate_limited",
		RequesterIP: "1.2.3.4",
		TxHash:      "AABBCCDD",
		Status:      "confirmed",
		RequestedAt: now,
		CompletedAt: now,
	}

	id, err := s.RecordDrip(t.Context(), rec)
	if err != nil {
		t.Fatalf("RecordDrip: %v", err)
	}
	if id <= 0 {
		t.Errorf("id: got %d, want > 0", id)
	}

	var got DripRecord
	var gotID int64
	err = s.db.QueryRowContext(context.Background(),
		`SELECT id, chain_id, address, coins, tier, requester_ip, tx_hash, status, requested_at, completed_at
		 FROM drips WHERE id = ?`, id,
	).Scan(&gotID, &got.ChainID, &got.Address, &got.Coins, &got.Tier,
		&got.RequesterIP, &got.TxHash, &got.Status, &got.RequestedAt, &got.CompletedAt)
	if err != nil {
		t.Fatalf("query back: %v", err)
	}

	if got.ChainID != rec.ChainID {
		t.Errorf("ChainID: got %q, want %q", got.ChainID, rec.ChainID)
	}
	if got.Address != rec.Address {
		t.Errorf("Address: got %q, want %q", got.Address, rec.Address)
	}
	if got.Coins != rec.Coins {
		t.Errorf("Coins: got %q, want %q", got.Coins, rec.Coins)
	}
	if got.TxHash != rec.TxHash {
		t.Errorf("TxHash: got %q, want %q", got.TxHash, rec.TxHash)
	}
	if got.Status != rec.Status {
		t.Errorf("Status: got %q, want %q", got.Status, rec.Status)
	}
}
