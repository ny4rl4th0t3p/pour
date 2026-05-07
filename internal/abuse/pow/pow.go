package pow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/altcha-org/altcha-lib-go"
)

// Difficulty is the maximum number the solver must iterate to.
// Higher values take longer to solve on the client.
type Difficulty int

const (
	DifficultyEasy   Difficulty = 5_000
	DifficultyMedium Difficulty = 50_000
	DifficultyHard   Difficulty = 500_000

	challengeTTL = 10 * time.Minute
)

// Issuer creates and verifies stateless Altcha PoW challenges signed with an HMAC key.
// The key is fetched on every operation so that admin token rotations take effect
// immediately without a server restart.
type Issuer struct {
	hmacKeyFn func() []byte
}

// New creates an Issuer. hmacKeyFn is called on every challenge creation and
// verification, so rotating the admin token immediately invalidates outstanding
// challenges and signs new ones with the updated key.
func New(hmacKeyFn func() []byte) *Issuer {
	return &Issuer{hmacKeyFn: hmacKeyFn}
}

// NewChallenge issues a new Altcha challenge and returns it as a JSON string.
// The JSON is suitable for embedding directly in the GET /v1/pow/challenge response.
func (i *Issuer) NewChallenge(d Difficulty) (string, error) {
	expires := time.Now().Add(challengeTTL)
	ch, err := altcha.CreateChallenge(altcha.ChallengeOptions{
		HMACKey:   string(i.hmacKeyFn()),
		MaxNumber: int64(d),
		Algorithm: altcha.SHA256,
		Expires:   &expires,
	})
	if err != nil {
		return "", fmt.Errorf("pow: create challenge: %w", err)
	}
	b, err := json.Marshal(ch)
	if err != nil {
		return "", fmt.Errorf("pow: marshal challenge: %w", err)
	}
	return string(b), nil
}

// Verify checks that solution (base64-encoded Altcha payload from the widget)
// is a valid, unexpired solution for the given challenge JSON.
// challenge is validated via the HMAC embedded in solution; passing it here
// makes the call site self-documenting and allows future binding checks.
func (i *Issuer) Verify(_, solution string) (bool, error) {
	ok, err := altcha.VerifySolutionSafe(solution, string(i.hmacKeyFn()), true)
	if err != nil {
		return false, fmt.Errorf("pow: verify: %w", err)
	}
	return ok, nil
}

// ParseDifficulty converts a config difficulty string to a Difficulty value.
// Accepts "easy", "medium", "hard", or a positive integer string.
func ParseDifficulty(s string) (Difficulty, error) {
	switch s {
	case "easy":
		return DifficultyEasy, nil
	case "medium":
		return DifficultyMedium, nil
	case "hard":
		return DifficultyHard, nil
	default:
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("pow: invalid difficulty %q: must be easy, medium, hard, or a positive integer", s)
		}
		return Difficulty(n), nil
	}
}
