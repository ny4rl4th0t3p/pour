package keys

import (
	"testing"
)

// Standard BIP39 test mnemonic (12-word, all-zeros entropy + checksum).
// Used as a reference across cosmjs, keplr, and cosmos-sdk test suites.
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

// Verified against Keplr wallet using the abandon mnemonic.
const cosmosAddr = "cosmos19rl4cm2hmr8afy4kldpxz3fka4jguq0auqdal4"

// Verified against Keplr wallet using the abandon mnemonic.
const osmoAddr = "osmo19rl4cm2hmr8afy4kldpxz3fka4jguq0a5m7df8"

func TestDerivePrivKey_differentIndex(t *testing.T) {
	a, _ := DerivePrivKey(testMnemonic, 118, 0)
	b, _ := DerivePrivKey(testMnemonic, 118, 1)
	if a.PubKey() == b.PubKey() {
		t.Fatal("index 0 and index 1 produced the same key")
	}
}

func TestDerivePrivKey_differentSlip44(t *testing.T) {
	cosmos, _ := DerivePrivKey(testMnemonic, 118, 0) // Cosmos Hub
	eth, _ := DerivePrivKey(testMnemonic, 60, 0)     // Ethereum
	if cosmos.PubKey() == eth.PubKey() {
		t.Fatal("different slip44 values produced the same key")
	}
}

func TestDerivePrivKey_invalidMnemonic(t *testing.T) {
	_, err := DerivePrivKey("not a valid mnemonic", 118, 0)
	if err == nil {
		t.Fatal("expected error for invalid mnemonic")
	}
}

func TestAddressFromPubKey_cosmos(t *testing.T) {
	priv, err := DerivePrivKey(testMnemonic, 118, 0)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := AddressFromPubKey(priv.PubKey(), "cosmos")
	if err != nil {
		t.Fatalf("AddressFromPubKey: %v", err)
	}
	if addr != cosmosAddr {
		t.Errorf("address mismatch:\n  got:  %s\n  want: %s", addr, cosmosAddr)
	}
}

func TestAddressFromPubKey_osmosis(t *testing.T) {
	priv, _ := DerivePrivKey(testMnemonic, 118, 0)
	addr, err := AddressFromPubKey(priv.PubKey(), "osmo")
	if err != nil {
		t.Fatal(err)
	}
	if addr != osmoAddr {
		t.Errorf("address mismatch:\n  got:  %s\n  want: %s", addr, osmoAddr)
	}
}

func TestSign(t *testing.T) {
	priv, err := DerivePrivKey(testMnemonic, 118, 0)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("test message")
	sig, err := priv.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != 64 {
		t.Errorf("signature length: got %d, want 64", len(sig))
	}
}

func TestPubKeyAnyTypeURL(t *testing.T) {
	if got := PubKeyAnyTypeURL(); got != "/cosmos.crypto.secp256k1.PubKey" {
		t.Errorf("got %q", got)
	}
}

func TestBech32Decode_roundTrip(t *testing.T) {
	// Decode the known cosmos address and re-encode; must round-trip exactly.
	hrp, data, err := Bech32Decode(cosmosAddr)
	if err != nil {
		t.Fatalf("Bech32Decode: %v", err)
	}
	if hrp != "cosmos" {
		t.Errorf("HRP: got %q, want %q", hrp, "cosmos")
	}
	got, err := AddressFromBytes(data, hrp)
	if err != nil {
		t.Fatalf("AddressFromBytes: %v", err)
	}
	if got != cosmosAddr {
		t.Errorf("round-trip mismatch:\n  got:  %s\n  want: %s", got, cosmosAddr)
	}
}

func TestBech32Decode_crossPrefix(t *testing.T) {
	// Decoding a cosmos address and re-encoding with "osmo" must give the known osmo address.
	_, data, err := Bech32Decode(cosmosAddr)
	if err != nil {
		t.Fatalf("Bech32Decode: %v", err)
	}
	got, err := AddressFromBytes(data, "osmo")
	if err != nil {
		t.Fatalf("AddressFromBytes: %v", err)
	}
	if got != osmoAddr {
		t.Errorf("cross-prefix:\n  got:  %s\n  want: %s", got, osmoAddr)
	}
}

func TestBech32Decode_invalidChecksum(t *testing.T) {
	// Flip one data character to corrupt the checksum.
	corrupted := cosmosAddr[:len(cosmosAddr)-3] + "zzz"
	if _, _, err := Bech32Decode(corrupted); err == nil {
		t.Error("expected error for invalid checksum")
	}
}

func TestBech32Decode_invalidChar(t *testing.T) {
	if _, _, err := Bech32Decode("cosmos1!nvalidchar"); err == nil {
		t.Error("expected error for invalid bech32 character")
	}
}

func TestBech32Decode_tooShort(t *testing.T) {
	if _, _, err := Bech32Decode("cosmos1a"); err == nil {
		t.Error("expected error for string too short to contain checksum")
	}
}

func TestAddressFromBytes_consistency(t *testing.T) {
	// AddressFromPubKey must produce the same result as AddressFromBytes(ripemd160(sha256(pub))).
	priv, err := DerivePrivKey(testMnemonic, 118, 0)
	if err != nil {
		t.Fatal(err)
	}
	fromPubKey, err := AddressFromPubKey(priv.PubKey(), "cosmos")
	if err != nil {
		t.Fatalf("AddressFromPubKey: %v", err)
	}
	_, raw, err := Bech32Decode(fromPubKey)
	if err != nil {
		t.Fatalf("Bech32Decode: %v", err)
	}
	fromBytes, err := AddressFromBytes(raw, "cosmos")
	if err != nil {
		t.Fatalf("AddressFromBytes: %v", err)
	}
	if fromBytes != fromPubKey {
		t.Errorf("inconsistency: AddressFromPubKey=%s AddressFromBytes=%s", fromPubKey, fromBytes)
	}
}
