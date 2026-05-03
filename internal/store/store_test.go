package store

import (
	"context"
	"path/filepath"
	"testing"
)

var expectedTables = []string{
	"rate_limits",
	"drips",
	"api_keys",
	"chain_gas_cache",
	"pending_changes",
}

func TestNew_createsSchema(t *testing.T) {
	s, err := New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	for _, table := range expectedTables {
		var name string
		err := s.db.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestNew_createsIndexes(t *testing.T) {
	s, err := New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	indexes := []string{
		"idx_rate_limits_lookup",
		"idx_drips_address_chain",
		"idx_drips_chain_status",
	}
	for _, idx := range indexes {
		var name string
		err := s.db.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q not found: %v", idx, err)
		}
	}
}

func TestMigrate_idempotent(t *testing.T) {
	s, err := New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}

	// Running migrate again on the same db must be a no-op.
	if err := migrate(context.Background(), s.db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	s.Close()
}

func TestMigrate_order(t *testing.T) {
	s, err := New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	rows, err := s.db.QueryContext(context.Background(), `SELECT name FROM schema_migrations ORDER BY applied_at, name`)
	if err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	defer rows.Close()

	want := []string{
		"001_rate_limits.sql",
		"002_drips.sql",
		"003_api_keys.sql",
		"004_chain_gas_cache.sql",
		"005_pending_changes.sql",
	}
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("migration count: got %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("migration[%d]: got %s, want %s", i, got[i], want[i])
		}
	}
}
