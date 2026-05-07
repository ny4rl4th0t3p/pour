package pow

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/altcha-org/altcha-lib-go"
)

var testKey = []byte("test-hmac-key-for-unit-tests")

// buildSolution creates a base64-encoded Altcha solution payload for the given
// challenge JSON. Used only in tests — SolveChallenge is intentionally slow by
// design; DifficultyEasy keeps test runtime under a second.
func buildSolution(t *testing.T, challengeJSON string) string {
	t.Helper()
	var ch altcha.Challenge
	if err := json.Unmarshal([]byte(challengeJSON), &ch); err != nil {
		t.Fatalf("unmarshal challenge: %v", err)
	}
	sol, err := altcha.SolveChallenge(ch.Challenge, ch.Salt, altcha.Algorithm(ch.Algorithm), int(DifficultyEasy), 0, nil)
	if err != nil || sol == nil {
		t.Fatalf("SolveChallenge: %v", err)
	}
	payload := altcha.Payload{
		Algorithm: ch.Algorithm,
		Challenge: ch.Challenge,
		Number:    int64(sol.Number),
		Salt:      ch.Salt,
		Signature: ch.Signature,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestRoundTrip(t *testing.T) {
	issuer := New(func() []byte { return testKey })
	challengeJSON, err := issuer.NewChallenge(DifficultyEasy)
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	if challengeJSON == "" {
		t.Fatal("expected non-empty challenge JSON")
	}

	solution := buildSolution(t, challengeJSON)
	ok, err := issuer.Verify(challengeJSON, solution)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("valid solution should verify as true")
	}
}

func TestVerify_tampered(t *testing.T) {
	issuer := New(func() []byte { return testKey })
	challengeJSON, err := issuer.NewChallenge(DifficultyEasy)
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}

	solution := buildSolution(t, challengeJSON)

	// Decode, corrupt the signature field, re-encode.
	raw, err := base64.StdEncoding.DecodeString(solution)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	corrupted := base64.StdEncoding.EncodeToString(append(raw[:len(raw)-4], []byte("XXXX")...))

	ok, _ := issuer.Verify(challengeJSON, corrupted)
	if ok {
		t.Error("tampered solution should not verify")
	}
}

func TestVerify_wrongKey(t *testing.T) {
	issuer := New(func() []byte { return testKey })
	challengeJSON, err := issuer.NewChallenge(DifficultyEasy)
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	solution := buildSolution(t, challengeJSON)

	other := New(func() []byte { return []byte("different-key") })
	ok, _ := other.Verify(challengeJSON, solution)
	if ok {
		t.Error("solution signed with different key should not verify")
	}
}

func TestParseDifficulty(t *testing.T) {
	cases := []struct {
		input string
		want  Difficulty
		isErr bool
	}{
		{"easy", DifficultyEasy, false},
		{"medium", DifficultyMedium, false},
		{"hard", DifficultyHard, false},
		{"10000", 10000, false},
		{"1", 1, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"notanumber", 0, true},
		{"", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseDifficulty(tc.input)
			if tc.isErr {
				if err == nil {
					t.Errorf("ParseDifficulty(%q): expected error, got %v", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDifficulty(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseDifficulty(%q): got %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewChallenge_containsExpectedFields(t *testing.T) {
	issuer := New(func() []byte { return testKey })
	challengeJSON, err := issuer.NewChallenge(DifficultyMedium)
	if err != nil {
		t.Fatalf("NewChallenge: %v", err)
	}
	for _, field := range []string{"algorithm", "challenge", "salt", "signature"} {
		if !strings.Contains(challengeJSON, field) {
			t.Errorf("challenge JSON missing field %q", field)
		}
	}
}
