package ibc

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Denom computes the IBC denom hash for a token transferred over a single hop.
// The result is "ibc/" + uppercase hex SHA256 of "<port>/<channel>/<baseDenom>".
func Denom(port, channel, baseDenom string) string {
	path := fmt.Sprintf("%s/%s/%s", port, channel, baseDenom)
	hash := sha256.Sum256([]byte(path))
	return "ibc/" + strings.ToUpper(fmt.Sprintf("%x", hash))
}
