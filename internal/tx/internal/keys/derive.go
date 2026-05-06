package keys

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ripemd160" //nolint:staticcheck,gosec // G507: RIPEMD-160 is mandated by the Cosmos address spec (SHA256→RIPEMD160→bech32)

	"github.com/cosmos/go-bip39"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// DerivePrivKey derives a secp256k1 private key from a BIP39 mnemonic.
// HD path: m/44'/<slip44>'/0'/0/<index>
func DerivePrivKey(mnemonic string, slip44, index uint32) (*PrivKey, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, errors.New("keys: invalid mnemonic")
	}
	seed := bip39.NewSeed(mnemonic, "")

	mac := hmac.New(sha512.New, []byte("Bitcoin seed"))
	mac.Write(seed)
	I := mac.Sum(nil)
	keyBytes, chainCode := I[:32], I[32:]

	steps := []struct {
		index    uint32
		hardened bool
	}{
		{44, true},
		{slip44, true},
		{0, true},
		{0, false},
		{index, false},
	}

	var err error
	for _, s := range steps {
		keyBytes, chainCode, err = deriveChild(keyBytes, chainCode, s.index, s.hardened)
		if err != nil {
			return nil, fmt.Errorf("keys: bip32 derive: %w", err)
		}
	}

	return &PrivKey{key: secp256k1.PrivKeyFromBytes(keyBytes)}, nil
}

// deriveChild performs one BIP32 child key derivation step.
const (
	bip32DataLen     = 37         // 1 prefix + 32 key + 4 index
	hardenedKeyStart = 0x80000000 // BIP32 hardened child key flag
)

func deriveChild(privBytes, chainCode []byte, index uint32, hardened bool) (keyBytes, newChainCode []byte, err error) {
	data := make([]byte, bip32DataLen)
	if hardened {
		copy(data[1:33], privBytes)
		binary.BigEndian.PutUint32(data[33:], index|hardenedKeyStart)
	} else {
		priv := secp256k1.PrivKeyFromBytes(privBytes)
		copy(data[:33], priv.PubKey().SerializeCompressed())
		binary.BigEndian.PutUint32(data[33:], index)
	}

	mac := hmac.New(sha512.New, chainCode)
	mac.Write(data)
	I := mac.Sum(nil)
	IL, IR := I[:32], I[32:]

	var il, parent secp256k1.ModNScalar
	if il.SetByteSlice(IL) {
		return nil, nil, errors.New("IL >= curve order")
	}
	parent.SetByteSlice(privBytes)
	il.Add(&parent)

	b := il.Bytes()
	return b[:], IR, nil
}

// AddressFromPubKey derives a Cosmos bech32 address from a compressed public key.
// Algorithm: SHA256(pubKey) → RIPEMD160 → bech32(prefix, bytes).
func AddressFromPubKey(pub PubKey, bech32Prefix string) (string, error) {
	sha256Hash := sha256.Sum256(pub[:])
	h := ripemd160.New() //nolint:gosec // G406: same as import — protocol requirement
	h.Write(sha256Hash[:])
	return AddressFromBytes(h.Sum(nil), bech32Prefix)
}

// PubKeyAnyTypeURL returns the protobuf Any type URL for a Cosmos secp256k1 public key.
func PubKeyAnyTypeURL() string {
	return "/cosmos.crypto.secp256k1.PubKey"
}

// AddressFromBytes bech32-encodes a raw address hash (20 bytes) with the given prefix.
// Unlike AddressFromPubKey this skips the SHA256→RIPEMD160 step and encodes the bytes directly.
func AddressFromBytes(addrBytes []byte, bech32Prefix string) (string, error) {
	converted, err := bech32ConvertBits(addrBytes, 8, 5, true)
	if err != nil {
		return "", fmt.Errorf("keys: %w", err)
	}
	return bech32Encode(bech32Prefix, converted), nil
}

// Bech32Decode decodes a bech32 string and returns the HRP and the decoded data bytes.
func Bech32Decode(s string) (hrp string, data []byte, err error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	sep := strings.LastIndex(lower, "1")
	if sep < 1 || sep+int(bech32ChecksumLen)+1 > len(lower) {
		return "", nil, errors.New("bech32: missing separator or too short")
	}
	hrp = lower[:sep]
	encoded := lower[sep+1:]

	decoded := make([]byte, len(encoded))
	for i, c := range encoded {
		idx := strings.IndexRune(bech32Charset, c)
		if idx < 0 {
			return "", nil, fmt.Errorf("bech32: invalid char %q at position %d", c, i)
		}
		decoded[i] = byte(idx) //nolint:gosec // G115: idx is 0–31 (bech32Charset has 32 chars)
	}

	combined := append(bech32HRPExpand(hrp), decoded...)
	if bech32Polymod(combined) != 1 {
		return "", nil, errors.New("bech32: invalid checksum")
	}

	data5 := decoded[:len(decoded)-int(bech32ChecksumLen)]
	data, err = bech32ConvertBits(data5, 5, 8, false)
	if err != nil {
		return "", nil, err
	}
	return hrp, data, nil
}

// ---- inline bech32 (BIP173) ----

const (
	bech32Charset          = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	bech32WordMask         = 31 // (1<<5)-1: masks a single 5-bit bech32 word
	bech32ChecksumLen uint = 6
)

var bech32Generator = []uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

func bech32Polymod(values []byte) uint32 {
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i, g := range bech32Generator {
			if (top>>uint(i))&1 != 0 {
				chk ^= g
			}
		}
	}
	return chk
}

func bech32HRPExpand(hrp string) []byte {
	out := make([]byte, len(hrp)*2+1)
	for i := range hrp {
		out[i] = hrp[i] >> 5
		out[len(hrp)+1+i] = hrp[i] & 31
	}
	return out
}

func bech32Checksum(hrp string, data []byte) []byte {
	input := append(bech32HRPExpand(hrp), data...)
	input = append(input, 0, 0, 0, 0, 0, 0)
	poly := bech32Polymod(input) ^ 1
	out := make([]byte, bech32ChecksumLen)
	for i := range bech32ChecksumLen {
		out[i] = byte((poly >> (5 * (bech32ChecksumLen - 1 - i))) & bech32WordMask)
	}
	return out
}

func bech32Encode(hrp string, data []byte) string {
	combined := append([]byte(nil), data...)
	combined = append(combined, bech32Checksum(hrp, data)...)
	var sb strings.Builder
	sb.WriteString(hrp)
	sb.WriteByte('1')
	for _, b := range combined {
		sb.WriteByte(bech32Charset[b])
	}
	return sb.String()
}

func bech32ConvertBits(data []byte, fromBits, toBits uint, pad bool) ([]byte, error) {
	acc, bits := 0, uint(0)
	maxv := (1 << toBits) - 1
	var out []byte
	for _, v := range data {
		acc = (acc << fromBits) | int(v)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			out = append(out, byte((acc>>bits)&maxv)) //nolint:gosec // G115: value bounded to 0-maxv by & maxv; gosec cannot infer this
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte((acc<<(toBits-bits))&maxv)) //nolint:gosec // G115: same — bounded by & maxv
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, errors.New("bech32: invalid padding")
	}
	return out, nil
}
