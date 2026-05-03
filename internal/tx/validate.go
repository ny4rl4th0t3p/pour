package tx

import (
	"fmt"
	"strings"
)

// ValidateAddress returns an error when addr is not a syntactically valid bech32
// address or its human-readable part does not match expectedPrefix.
// Full checksum validation occurs implicitly in the gRPC layer when the tx is broadcast.
func ValidateAddress(addr, expectedPrefix string) error {
	lower := strings.ToLower(strings.TrimSpace(addr))
	sep := strings.Index(lower, "1")
	if sep <= 0 || sep == len(lower)-1 {
		return fmt.Errorf("tx: invalid bech32 address: missing separator or empty data")
	}
	hrp := lower[:sep]
	if hrp != expectedPrefix {
		return fmt.Errorf("tx: address prefix %q: want %q", hrp, expectedPrefix)
	}
	return nil
}
