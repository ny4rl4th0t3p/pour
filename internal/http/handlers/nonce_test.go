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

type stubNonceIssuer struct {
	nonce string
	err   error
}

func (s *stubNonceIssuer) Issue() (string, error) { return s.nonce, s.err }

func TestSignNonce_disabled(t *testing.T) {
	h := New(Deps{
		Source:   testSource,
		AbuseCfg: config.AbuseConfig{SignatureChallenge: config.AbuseSignatureChallengeConfig{Enabled: false}},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/sign/nonce", http.NoBody)
	h.SignNonce(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func TestSignNonce_enabled(t *testing.T) {
	h := New(Deps{
		Source:      testSource,
		AbuseCfg:    config.AbuseConfig{SignatureChallenge: config.AbuseSignatureChallengeConfig{Enabled: true}},
		NonceIssuer: &stubNonceIssuer{nonce: "deadbeef12345678"},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/sign/nonce", http.NoBody)
	h.SignNonce(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp pourapi.NonceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Nonce != "deadbeef12345678" {
		t.Errorf("nonce: got %q, want deadbeef12345678", resp.Nonce)
	}
}

func TestSignNonce_issuerError(t *testing.T) {
	h := New(Deps{
		Source:      testSource,
		AbuseCfg:    config.AbuseConfig{SignatureChallenge: config.AbuseSignatureChallengeConfig{Enabled: true}},
		NonceIssuer: &stubNonceIssuer{err: errors.New("entropy failure")},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/sign/nonce", http.NoBody)
	h.SignNonce(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}
