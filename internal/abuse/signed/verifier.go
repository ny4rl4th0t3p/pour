package signed

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

const (
	compactSigLen       = 64 // R||S without recovery byte
	compactSigWithRecov = 65 // R||S with recovery byte prepended
)

// Verifier verifies secp256k1 signed challenges in Cosmos ADR-036 format.
type Verifier struct{}

// New returns a Verifier.
func New() *Verifier { return &Verifier{} }

// Verify checks that sigB64 is a valid ADR-036 signature over challenge,
// signed by the key that controls address.
//
// pubkeyB64 is a base64-encoded 33-byte compressed secp256k1 public key.
// sigB64 is a base64-encoded 64-byte (or 65-byte with recovery prefix) signature.
//
// Returns (false, nil) on a well-formed but invalid/mismatched credential.
// Returns (false, err) on malformed input.
func (*Verifier) Verify(address, pubkeyB64, sigB64, challenge string) (bool, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(pubkeyB64)
	if err != nil {
		return false, fmt.Errorf("signed: decode pubkey: %w", err)
	}
	pubKey, err := secp256k1.ParsePubKey(pubBytes)
	if err != nil {
		return false, fmt.Errorf("signed: parse pubkey: %w", err)
	}

	hrp := hrpFromAddress(address)
	if hrp == "" {
		return false, errors.New("signed: cannot extract HRP from address")
	}
	expected, err := tx.AddressFromPubKey(pubBytes, hrp)
	if err != nil {
		return false, fmt.Errorf("signed: derive address: %w", err)
	}
	if !strings.EqualFold(expected, strings.TrimSpace(address)) {
		return false, nil
	}

	signBytes, err := adr036SignBytes(address, challenge)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(signBytes)

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false, fmt.Errorf("signed: decode signature: %w", err)
	}
	sig, err := parseSig(sigBytes)
	if err != nil {
		return false, err
	}

	return sig.Verify(hash[:], pubKey), nil
}

// hrpFromAddress extracts the bech32 human-readable part from a Cosmos address.
func hrpFromAddress(addr string) string {
	lower := strings.ToLower(strings.TrimSpace(addr))
	sep := strings.Index(lower, "1")
	if sep <= 0 {
		return ""
	}
	return lower[:sep]
}

// adr036 JSON types — fields are in strict alphabetical order for amino canonical encoding.
type adr036Doc struct {
	AccountNumber string      `json:"account_number"`
	ChainID       string      `json:"chain_id"`
	Fee           adr036Fee   `json:"fee"`
	Memo          string      `json:"memo"`
	Msgs          []adr036Msg `json:"msgs"`
	Sequence      string      `json:"sequence"`
}

type adr036Fee struct {
	Amount json.RawMessage `json:"amount"`
	Gas    string          `json:"gas"`
}

type adr036Msg struct {
	Type  string         `json:"type"`
	Value adr036MsgValue `json:"value"`
}

type adr036MsgValue struct {
	Data   string `json:"data"`
	Signer string `json:"signer"`
}

// adr036SignBytes builds the canonical amino JSON bytes that Cosmos wallets sign
// for an ADR-036 arbitrary-data message.
func adr036SignBytes(address, challenge string) ([]byte, error) {
	doc := adr036Doc{
		AccountNumber: "0",
		ChainID:       "",
		Fee:           adr036Fee{Amount: json.RawMessage("[]"), Gas: "0"},
		Memo:          "",
		Msgs: []adr036Msg{{
			Type: "sign/MsgSignData",
			Value: adr036MsgValue{
				Data:   base64.StdEncoding.EncodeToString([]byte(challenge)),
				Signer: address,
			},
		}},
		Sequence: "0",
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("signed: marshal adr036: %w", err)
	}
	return b, nil
}

// parseSig decodes a 64-byte R||S (or 65-byte with recovery byte) compact signature
// into a DER-encoded *ecdsa.Signature for verification.
func parseSig(sigBytes []byte) (*ecdsa.Signature, error) {
	if len(sigBytes) == compactSigWithRecov {
		sigBytes = sigBytes[1:]
	}
	if len(sigBytes) != compactSigLen {
		return nil, fmt.Errorf("signed: signature must be %d or %d bytes, got %d",
			compactSigLen, compactSigWithRecov, len(sigBytes))
	}
	der, err := compactToDER(sigBytes)
	if err != nil {
		return nil, err
	}
	return ecdsa.ParseDERSignature(der)
}

// compactToDER converts a 64-byte compact R||S signature to DER encoding.
func compactToDER(sig []byte) ([]byte, error) {
	if len(sig) != compactSigLen {
		return nil, fmt.Errorf("signed: expected %d-byte compact sig, got %d", compactSigLen, len(sig))
	}
	r := padDER(sig[:32])
	s := padDER(sig[32:])
	body := make([]byte, 0, 4+len(r)+len(s))
	body = append(body, 0x02, byte(len(r))) //nolint:gosec // G115: ≤33 bytes after padDER
	body = append(body, r...)
	body = append(body, 0x02, byte(len(s))) //nolint:gosec // G115: same
	body = append(body, s...)
	return append([]byte{0x30, byte(len(body))}, body...), nil //nolint:gosec // G115: body ≤70 bytes
}

// padDER strips leading zeros and prepends 0x00 if the high bit is set (DER sign convention).
func padDER(b []byte) []byte {
	i := 0
	for i < len(b)-1 && b[i] == 0 {
		i++
	}
	b = b[i:]
	if b[0]&0x80 != 0 {
		return append([]byte{0x00}, b...)
	}
	return b
}
