package apikey

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := store.New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s)
}

func TestCreate_Authenticate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, secret, err := s.Create(ctx, CreateParams{
		Label:      "test key",
		ChainScope: []string{"osmosis-1"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" || secret == "" {
		t.Fatal("expected non-empty id and secret")
	}
	if !hasPrefix(secret, secretPrefix) {
		t.Errorf("secret missing prefix: %q", secret[:min(len(secret), 20)])
	}

	k, err := s.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if k.ID != id {
		t.Errorf("ID: got %q, want %q", k.ID, id)
	}
	if k.Label != "test key" {
		t.Errorf("Label: got %q", k.Label)
	}
}

func TestAuthenticate_wrongSecret(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, secret, err := s.Create(ctx, CreateParams{ChainScope: []string{"*"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Flip the last character of the secret body.
	tampered := secret[:len(secret)-1] + "X"
	_, err = s.Authenticate(ctx, tampered)
	if !errors.Is(err, ErrInvalidSecret) {
		t.Errorf("wrong secret: got %v, want ErrInvalidSecret", err)
	}
}

func TestAuthenticate_paddingBitTamper(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, secret, err := s.Create(ctx, CreateParams{ChainScope: []string{"*"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The canonical last char of a 43-char base64url body always has its two least-significant
	// bits = 00 (only 4 data bits fit; 2 are padding). Incrementing its base64url alphabet index
	// by 1 changes only those padding bits, so the decoded body bytes are identical. Without the
	// canonical encoding check this case would silently authenticate — that was the original bug.
	const b64url = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := secret[len(secret)-1]
	var charIdx int
	for i := range b64url {
		if b64url[i] == last {
			charIdx = i
			break
		}
	}
	tampered := secret[:len(secret)-1] + string(b64url[charIdx+1])

	_, err = s.Authenticate(ctx, tampered)
	if !errors.Is(err, ErrInvalidSecret) {
		t.Errorf("padding-bit tamper: got %v, want ErrInvalidSecret", err)
	}
}

func TestAuthenticate_revokedKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, secret, err := s.Create(ctx, CreateParams{ChainScope: []string{"*"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Revoke(ctx, id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, err = s.Authenticate(ctx, secret)
	if !errors.Is(err, ErrRevoked) {
		t.Errorf("revoked: got %v, want ErrRevoked", err)
	}
}

func TestAuthenticate_expiredKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, secret, err := s.Create(ctx, CreateParams{
		ChainScope: []string{"*"},
		ExpiresAt:  &past,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = s.Authenticate(ctx, secret)
	if !errors.Is(err, ErrExpired) {
		t.Errorf("expired: got %v, want ErrExpired", err)
	}
}

func TestRevoke_notFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Revoke(context.Background(), "key_nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id1, _, err := s.Create(ctx, CreateParams{Label: "a", ChainScope: []string{"*"}})
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	id2, _, err := s.Create(ctx, CreateParams{Label: "b", ChainScope: []string{"*"}})
	if err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if err := s.Revoke(ctx, id2); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	keys, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List: got %d keys, want 1 (revoked excluded)", len(keys))
	}
	if keys[0].ID != id1 {
		t.Errorf("List: got ID %q, want %q", keys[0].ID, id1)
	}
}

func TestInScope(t *testing.T) {
	wildcard := &Key{ChainScope: []string{"*"}}
	if !wildcard.InScope("osmosis-1") {
		t.Error("wildcard should match any chain")
	}

	explicit := &Key{ChainScope: []string{"osmosis-1", "cosmos-1"}}
	if !explicit.InScope("osmosis-1") {
		t.Error("should match osmosis-1")
	}
	if explicit.InScope("stargaze-1") {
		t.Error("should not match stargaze-1")
	}
}

func TestDripsForChain(t *testing.T) {
	k := &Key{PerChainDrips: map[string]string{"osmosis-1": "5000000uosmo"}}
	if got := k.DripsForChain("osmosis-1"); got != "5000000uosmo" {
		t.Errorf("got %q, want 5000000uosmo", got)
	}
	if got := k.DripsForChain("cosmos-1"); got != "" {
		t.Errorf("unknown chain: got %q, want empty", got)
	}
}

func TestParseBearer(t *testing.T) {
	token, ok := ParseBearer("Bearer pour_key_abc123")
	if !ok || token != "pour_key_abc123" {
		t.Errorf("ParseBearer: got %q %v", token, ok)
	}
	if _, ok := ParseBearer("Bearer other_token"); ok {
		t.Error("non-pour_key_ bearer should not match")
	}
	if _, ok := ParseBearer("pour_key_abc123"); ok {
		t.Error("missing Bearer prefix should not match")
	}
	if _, ok := ParseBearer(""); ok {
		t.Error("empty header should not match")
	}
}

func TestCreate_perChainDrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	drips := map[string]string{"osmosis-1": "5000000uosmo"}
	_, secret, err := s.Create(ctx, CreateParams{
		ChainScope:    []string{"osmosis-1"},
		PerChainDrips: drips,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	k, err := s.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if k.DripsForChain("osmosis-1") != "5000000uosmo" {
		t.Errorf("DripsForChain: got %q", k.DripsForChain("osmosis-1"))
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
