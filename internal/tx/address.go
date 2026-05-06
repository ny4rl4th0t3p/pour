package tx

import (
	"fmt"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
)

// AddressFromPubKey derives the bech32 address for a 33-byte compressed secp256k1 public key.
// Algorithm: SHA256(pubBytes) → RIPEMD160 → bech32(prefix, …).
func AddressFromPubKey(pubBytes []byte, prefix string) (string, error) {
	if len(pubBytes) != 33 { //nolint:mnd // 33 = compressed secp256k1 pubkey length
		return "", fmt.Errorf("tx: pubkey must be 33 bytes compressed, got %d", len(pubBytes))
	}
	var pub keys.PubKey
	copy(pub[:], pubBytes)
	return keys.AddressFromPubKey(pub, prefix)
}

// DecodeAddressBytes bech32-decodes an address and returns the raw address hash bytes.
func DecodeAddressBytes(addr string) ([]byte, error) {
	_, data, err := keys.Bech32Decode(addr)
	if err != nil {
		return nil, fmt.Errorf("tx: decode address %q: %w", addr, err)
	}
	return data, nil
}

// AddressFromBytes bech32-encodes raw address hash bytes with the given prefix.
// Use this to re-encode an address under a different chain's bech32 prefix.
func AddressFromBytes(rawBytes []byte, prefix string) (string, error) {
	addr, err := keys.AddressFromBytes(rawBytes, prefix)
	if err != nil {
		return "", fmt.Errorf("tx: encode address: %w", err)
	}
	return addr, nil
}
