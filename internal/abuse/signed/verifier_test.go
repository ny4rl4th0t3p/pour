package signed

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// newTestKey generates a secp256k1 key pair and derives a bech32 address with prefix.
func newTestKey(t testing.TB, prefix string) (privKey *secp256k1.PrivateKey, addr string) {
	t.Helper()
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	pubBytes := privKey.PubKey().SerializeCompressed()
	addr, err = tx.AddressFromPubKey(pubBytes, prefix)
	if err != nil {
		t.Fatalf("AddressFromPubKey: %v", err)
	}
	return privKey, addr
}

// signADR036 builds and signs the ADR-036 bytes with privKey. Returns base64 R||S signature.
func signADR036(t testing.TB, privKey *secp256k1.PrivateKey, address, challenge string) string {
	t.Helper()
	signBytes, err := adr036SignBytes(address, challenge)
	if err != nil {
		t.Fatalf("adr036SignBytes: %v", err)
	}
	hash := sha256.Sum256(signBytes)
	compact := ecdsa.SignCompact(privKey, hash[:], false)
	return base64.StdEncoding.EncodeToString(compact[1:]) // strip recovery byte
}

func TestVerify_roundTrip(t *testing.T) {
	v := New()
	privKey, addr := newTestKey(t, "cosmos")
	pubB64 := base64.StdEncoding.EncodeToString(privKey.PubKey().SerializeCompressed())
	challenge := "test-challenge-abc"

	ok, err := v.Verify(addr, pubB64, signADR036(t, privKey, addr, challenge), challenge)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("expected Verify to return true for a valid credential")
	}
}

func TestVerify_wrongChallenge(t *testing.T) {
	v := New()
	privKey, addr := newTestKey(t, "cosmos")
	pubB64 := base64.StdEncoding.EncodeToString(privKey.PubKey().SerializeCompressed())

	sigB64 := signADR036(t, privKey, addr, "challenge-A")
	ok, err := v.Verify(addr, pubB64, sigB64, "challenge-B")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("expected false when challenge does not match signed bytes")
	}
}

func TestVerify_addressMismatch(t *testing.T) {
	v := New()
	key1, addr1 := newTestKey(t, "cosmos")
	key2, _ := newTestKey(t, "cosmos")
	pub2B64 := base64.StdEncoding.EncodeToString(key2.PubKey().SerializeCompressed())

	// key2's pubkey does not correspond to addr1; verifier should reject before sig check.
	sigB64 := signADR036(t, key1, addr1, "challenge")
	ok, err := v.Verify(addr1, pub2B64, sigB64, "challenge")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("expected false when pubkey does not correspond to address")
	}
}

func TestVerify_invalidPubkeyBase64(t *testing.T) {
	v := New()
	_, err := v.Verify("cosmos1abc", "not!!valid!!base64", "sig", "challenge")
	if err == nil {
		t.Error("expected error for invalid base64 pubkey")
	}
}

func TestVerify_invalidSigBase64(t *testing.T) {
	v := New()
	privKey, addr := newTestKey(t, "cosmos")
	pubB64 := base64.StdEncoding.EncodeToString(privKey.PubKey().SerializeCompressed())
	_, err := v.Verify(addr, pubB64, "not!!valid!!base64", "challenge")
	if err == nil {
		t.Error("expected error for invalid base64 signature")
	}
}

func TestVerify_differentPrefix(t *testing.T) {
	v := New()
	privKey, osmoAddr := newTestKey(t, "osmo")
	pubB64 := base64.StdEncoding.EncodeToString(privKey.PubKey().SerializeCompressed())
	challenge := "cross-chain-challenge"

	ok, err := v.Verify(osmoAddr, pubB64, signADR036(t, privKey, osmoAddr, challenge), challenge)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("expected Verify to succeed with non-cosmos prefix")
	}
}

// --- predicate tests ---

type stubQuerier struct {
	// balance maps "address/denom" → amount string
	balance map[string]string
}

func (s *stubQuerier) QueryBalance(_ context.Context, address, denom string) (tx.Coin, error) {
	amount := s.balance[address+"/"+denom]
	if amount == "" {
		amount = "0"
	}
	return tx.Coin{Denom: denom, Amount: amount}, nil
}

func TestPredicateChecker_none(t *testing.T) {
	pc := NewPredicateChecker(nil, nil)
	if err := pc.Check(context.Background(), PredicateNone, "", "addr", ""); err != nil {
		t.Errorf("predicate=none should always pass: %v", err)
	}
}

func TestPredicateChecker_hasBalance_pass(t *testing.T) {
	_, addr := newTestKey(t, "cosmos")
	// reencodeAddress("cosmos1...", "cosmos") == addr, so stub key equals addr.
	q := &stubQuerier{balance: map[string]string{addr + "/uatom": "2000000"}}
	pc := NewPredicateChecker(
		map[string]BalanceQuerier{"cosmoshub-4": q},
		map[string]string{"cosmoshub-4": "cosmos"},
	)
	err := pc.Check(context.Background(), PredicateHasBalance, "cosmoshub-4", addr, "1000000uatom")
	if err != nil {
		t.Errorf("expected pass (balance 2000000 >= min 1000000): %v", err)
	}
}

func TestPredicateChecker_hasBalance_fail(t *testing.T) {
	_, addr := newTestKey(t, "cosmos")
	q := &stubQuerier{balance: map[string]string{addr + "/uatom": "500000"}}
	pc := NewPredicateChecker(
		map[string]BalanceQuerier{"cosmoshub-4": q},
		map[string]string{"cosmoshub-4": "cosmos"},
	)
	err := pc.Check(context.Background(), PredicateHasBalance, "cosmoshub-4", addr, "1000000uatom")
	if !errors.Is(err, ErrPredicateFailed) {
		t.Errorf("expected ErrPredicateFailed (balance 500000 < min 1000000), got %v", err)
	}
}

func TestPredicateChecker_hasBalance_unknownChain(t *testing.T) {
	pc := NewPredicateChecker(
		map[string]BalanceQuerier{},
		map[string]string{},
	)
	err := pc.Check(context.Background(), PredicateHasBalance, "unknown-1", "cosmos1abc", "1000000uatom")
	if !errors.Is(err, ErrChainNotAvailable) {
		t.Errorf("expected ErrChainNotAvailable, got %v", err)
	}
}

func TestPredicateChecker_unknownPredicate(t *testing.T) {
	pc := NewPredicateChecker(nil, nil)
	if err := pc.Check(context.Background(), "is_wizard", "", "addr", ""); err == nil {
		t.Error("expected error for unknown predicate")
	}
}
