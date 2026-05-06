package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

type stubPowIssuer struct {
	challenge string
	err       error
}

func (s *stubPowIssuer) NewChallenge() (string, error) { return s.challenge, s.err }

func TestPowChallenge_disabled(t *testing.T) {
	h := New(Deps{Source: testSource, AbuseCfg: config.AbuseConfig{PoW: config.AbusePoWConfig{Enabled: false}}})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/pow/challenge", http.NoBody)
	h.PowChallenge(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func TestPowChallenge_enabled(t *testing.T) {
	h := New(Deps{
		Source:    testSource,
		AbuseCfg:  config.AbuseConfig{PoW: config.AbusePoWConfig{Enabled: true}},
		PowIssuer: &stubPowIssuer{challenge: `{"algorithm":"SHA-256"}`},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/pow/challenge", http.NoBody)
	h.PowChallenge(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.ChallengeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Challenge == "" {
		t.Error("challenge: want non-empty")
	}
}

func TestPowChallenge_issuerError(t *testing.T) {
	h := New(Deps{
		Source:    testSource,
		AbuseCfg:  config.AbuseConfig{PoW: config.AbusePoWConfig{Enabled: true}},
		PowIssuer: &stubPowIssuer{err: errors.New("crypto failure")},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/pow/challenge", http.NoBody)
	h.PowChallenge(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}
