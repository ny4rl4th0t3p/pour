package keys

import (
	"crypto/sha256"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// PubKey is a 33-byte compressed secp256k1 public key.
type PubKey [33]byte

// Bytes returns the compressed public key bytes.
func (p PubKey) Bytes() []byte {
	b := make([]byte, len(p))
	copy(b, p[:])
	return b
}

// PrivKey wraps a secp256k1 private key.
type PrivKey struct {
	key *secp256k1.PrivateKey
}

// Sign SHA256-hashes msg and signs the digest.
// Returns a 64-byte compact signature (R || S), matching the Cosmos secp256k1 wire format.
func (k *PrivKey) Sign(msg []byte) ([]byte, error) {
	hash := sha256.Sum256(msg)
	compact := ecdsa.SignCompact(k.key, hash[:], false)
	return compact[1:], nil // strip recovery byte → R || S
}

// PubKey returns the compressed public key.
func (k *PrivKey) PubKey() PubKey {
	var out PubKey
	copy(out[:], k.key.PubKey().SerializeCompressed())
	return out
}
