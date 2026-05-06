package abuse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/apikey"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// --- stubs ---

type stubAPIKeys struct {
	key *apikey.Key
	err error
}

func (s *stubAPIKeys) Authenticate(_ context.Context, _ string) (*apikey.Key, error) {
	return s.key, s.err
}

type stubPow struct {
	ok  bool
	err error
}

func (s *stubPow) Verify(_, _ string) (bool, error) { return s.ok, s.err }

type stubNonce struct{ ok bool }

func (s *stubNonce) Consume(_ string) bool { return s.ok }

type stubSig struct {
	ok  bool
	err error
}

func (s *stubSig) Verify(_, _, _, _ string) (bool, error) { return s.ok, s.err }

type stubPredicate struct{ err error }

func (s *stubPredicate) Check(_ context.Context, _, _, _, _ string) error { return s.err }

type stubLimiter struct {
	checkErr        error
	checkAPIKeyErr  error
	checkAddressErr error
}

func (s *stubLimiter) Check(_ context.Context, _, _ string) error { return s.checkErr }
func (s *stubLimiter) CheckAPIKey(_ context.Context, _, _ string, _ int) error {
	return s.checkAPIKeyErr
}
func (s *stubLimiter) CheckAddress(_ context.Context, _, _, _ string, _, _ tx.Coin) error {
	return s.checkAddressErr
}

// --- helpers ---

func unauthGate(cfg config.AbuseConfig) *Gate {
	return New(cfg, &stubAPIKeys{err: errors.New("bad")}, &stubPow{ok: true},
		&stubNonce{ok: true}, &stubSig{ok: true}, &stubPredicate{}, &stubLimiter{})
}

func defaultCC() ChainContext {
	return ChainContext{
		ChainID:       "osmosis-1",
		KeyAlgo:       "secp256k1",
		DripAnonymous: tx.Coin{Amount: "1000000", Denom: "uosmo"},
		DripSigned:    tx.Coin{Amount: "5000000", Denom: "uosmo"},
		MaxPerDay:     tx.Coin{Amount: "10000000", Denom: "uosmo"},
	}
}

func newReq() *pourapi.PourRequest {
	return &pourapi.PourRequest{ChainID: "osmosis-1", Address: "osmo1abc"}
}

func newHTTPReq(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
}

// --- IP rate limit ---

func TestAdmit_IPRateLimit(t *testing.T) {
	cfg := config.AbuseConfig{}
	g := New(cfg, nil, nil, nil, nil, nil, &stubLimiter{checkErr: &ratelimit.ErrRateLimitExceeded{RetryAfter: 60}})
	r := newHTTPReq(t)
	_, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	var rl *ratelimit.ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}

// --- API key path ---

func TestAdmit_APIKey_success(t *testing.T) {
	key := &apikey.Key{ID: "key_abc", ChainScope: []string{"*"}}
	cfg := config.AbuseConfig{APIKeys: config.AbuseAPIKeysConfig{Enabled: true}}
	g := New(cfg, &stubAPIKeys{key: key}, nil, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)
	r.Header.Set("Authorization", "Bearer pour_key_abc")

	d, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if d.Mechanism != MechanismAPIKey {
		t.Errorf("mechanism: got %q, want %q", d.Mechanism, MechanismAPIKey)
	}
	if d.APIKeyID != "key_abc" {
		t.Errorf("APIKeyID: got %q, want %q", d.APIKeyID, "key_abc")
	}
}

func TestAdmit_APIKey_badToken(t *testing.T) {
	cfg := config.AbuseConfig{APIKeys: config.AbuseAPIKeysConfig{Enabled: true}}
	g := unauthGate(cfg)
	r := newHTTPReq(t)
	r.Header.Set("Authorization", "Bearer pour_key_bad")

	_, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated, got %v", err)
	}
}

func TestAdmit_APIKey_outOfScope(t *testing.T) {
	key := &apikey.Key{ID: "key_abc", ChainScope: []string{"cosmoshub-4"}}
	cfg := config.AbuseConfig{APIKeys: config.AbuseAPIKeysConfig{Enabled: true}}
	g := New(cfg, &stubAPIKeys{key: key}, nil, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)
	r.Header.Set("Authorization", "Bearer pour_key_abc")

	_, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestAdmit_APIKey_rateLimited(t *testing.T) {
	key := &apikey.Key{ID: "key_abc", ChainScope: []string{"*"}}
	cfg := config.AbuseConfig{APIKeys: config.AbuseAPIKeysConfig{Enabled: true}}
	rlErr := &ratelimit.ErrRateLimitExceeded{RetryAfter: 60}
	g := New(cfg, &stubAPIKeys{key: key}, nil, nil, nil, nil, &stubLimiter{checkAPIKeyErr: rlErr})
	r := newHTTPReq(t)
	r.Header.Set("Authorization", "Bearer pour_key_abc")

	_, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	var rl *ratelimit.ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestAdmit_APIKey_addressCap(t *testing.T) {
	key := &apikey.Key{ID: "key_abc", ChainScope: []string{"*"}}
	cfg := config.AbuseConfig{APIKeys: config.AbuseAPIKeysConfig{Enabled: true}}
	rlErr := &ratelimit.ErrRateLimitExceeded{RetryAfter: 60}
	g := New(cfg, &stubAPIKeys{key: key}, nil, nil, nil, nil, &stubLimiter{checkAddressErr: rlErr})
	r := newHTTPReq(t)
	r.Header.Set("Authorization", "Bearer pour_key_abc")

	_, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	var rl *ratelimit.ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestAdmit_APIKey_perChainDripUsed(t *testing.T) {
	key := &apikey.Key{
		ID:            "key_abc",
		ChainScope:    []string{"*"},
		PerChainDrips: map[string]string{"osmosis-1": "2000000uosmo"},
	}
	cfg := config.AbuseConfig{APIKeys: config.AbuseAPIKeysConfig{Enabled: true}}
	g := New(cfg, &stubAPIKeys{key: key}, nil, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)
	r.Header.Set("Authorization", "Bearer pour_key_abc")

	d, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if d.DripCoin.Amount != "2000000" {
		t.Errorf("DripCoin.Amount: got %q, want %q", d.DripCoin.Amount, "2000000")
	}
}

// --- signed path ---

func signedReq(nonce string) *pourapi.PourRequest {
	return &pourapi.PourRequest{
		ChainID: "osmosis-1",
		Address: "osmo1abc",
		Signature: &pourapi.SigCredential{
			Nonce:     nonce,
			Address:   "osmo1signer",
			Pubkey:    "pubkey",
			Signature: "sig",
		},
	}
}

func signedCfg() config.AbuseConfig {
	return config.AbuseConfig{
		SignatureChallenge: config.AbuseSignatureChallengeConfig{
			Enabled:          true,
			RequirePredicate: "none",
		},
	}
}

func TestAdmit_Signed_success(t *testing.T) {
	g := New(signedCfg(), nil, nil, &stubNonce{ok: true}, &stubSig{ok: true}, &stubPredicate{}, &stubLimiter{})
	r := newHTTPReq(t)

	d, err := g.Admit(t.Context(), r, signedReq("testnonce"), defaultCC())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if d.Mechanism != MechanismSigned {
		t.Errorf("mechanism: got %q, want %q", d.Mechanism, MechanismSigned)
	}
	if d.SignerAddress != "osmo1signer" {
		t.Errorf("SignerAddress: got %q, want %q", d.SignerAddress, "osmo1signer")
	}
}

func TestAdmit_Signed_missingNonce(t *testing.T) {
	g := New(signedCfg(), nil, nil, &stubNonce{ok: true}, &stubSig{ok: true}, &stubPredicate{}, &stubLimiter{})
	r := newHTTPReq(t)
	req := signedReq("")

	_, err := g.Admit(t.Context(), r, req, defaultCC())
	if !errors.Is(err, ErrNonceRequired) {
		t.Fatalf("expected ErrNonceRequired, got %v", err)
	}
}

func TestAdmit_Signed_badNonce(t *testing.T) {
	g := New(signedCfg(), nil, nil, &stubNonce{ok: false}, &stubSig{ok: true}, &stubPredicate{}, &stubLimiter{})
	r := newHTTPReq(t)

	_, err := g.Admit(t.Context(), r, signedReq("expired"), defaultCC())
	if !errors.Is(err, ErrBadNonce) {
		t.Fatalf("expected ErrBadNonce, got %v", err)
	}
}

func TestAdmit_Signed_badSignature(t *testing.T) {
	g := New(signedCfg(), nil, nil, &stubNonce{ok: true}, &stubSig{ok: false}, &stubPredicate{}, &stubLimiter{})
	r := newHTTPReq(t)

	_, err := g.Admit(t.Context(), r, signedReq("nonce"), defaultCC())
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("expected ErrBadSignature, got %v", err)
	}
}

func TestAdmit_Signed_predicateFailed(t *testing.T) {
	predErr := errors.New("not enough balance")
	g := New(signedCfg(), nil, nil, &stubNonce{ok: true}, &stubSig{ok: true}, &stubPredicate{err: predErr}, &stubLimiter{})
	r := newHTTPReq(t)

	_, err := g.Admit(t.Context(), r, signedReq("nonce"), defaultCC())
	if !errors.Is(err, ErrPredicateFailed) {
		t.Fatalf("expected ErrPredicateFailed, got %v", err)
	}
}

func TestAdmit_Signed_dripFallback(t *testing.T) {
	g := New(signedCfg(), nil, nil, &stubNonce{ok: true}, &stubSig{ok: true}, &stubPredicate{}, &stubLimiter{})
	r := newHTTPReq(t)
	cc := defaultCC()
	cc.DripSigned = tx.Coin{} // not configured

	d, err := g.Admit(t.Context(), r, signedReq("nonce"), cc)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if d.DripCoin != cc.DripAnonymous {
		t.Errorf("DripCoin: got %+v, want %+v (anonymous fallback)", d.DripCoin, cc.DripAnonymous)
	}
}

// --- PoW path ---

func powCfg() config.AbuseConfig {
	return config.AbuseConfig{PoW: config.AbusePoWConfig{Enabled: true}}
}

func powReq() *pourapi.PourRequest {
	return &pourapi.PourRequest{
		ChainID: "osmosis-1",
		Address: "osmo1abc",
		Pow:     &pourapi.PowCredential{Challenge: "ch", Solution: "sol"},
	}
}

func TestAdmit_PoW_success(t *testing.T) {
	g := New(powCfg(), nil, &stubPow{ok: true}, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)

	d, err := g.Admit(t.Context(), r, powReq(), defaultCC())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if d.Mechanism != MechanismPoW {
		t.Errorf("mechanism: got %q, want %q", d.Mechanism, MechanismPoW)
	}
}

func TestAdmit_PoW_missing(t *testing.T) {
	g := New(powCfg(), nil, &stubPow{ok: true}, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)
	req := &pourapi.PourRequest{ChainID: "osmosis-1", Address: "osmo1abc"} // no Pow field

	_, err := g.Admit(t.Context(), r, req, defaultCC())
	if !errors.Is(err, ErrPoWRequired) {
		t.Fatalf("expected ErrPoWRequired, got %v", err)
	}
}

func TestAdmit_PoW_badSolution(t *testing.T) {
	g := New(powCfg(), nil, &stubPow{ok: false}, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)

	_, err := g.Admit(t.Context(), r, powReq(), defaultCC())
	if !errors.Is(err, ErrBadPoW) {
		t.Fatalf("expected ErrBadPoW, got %v", err)
	}
}

func TestAdmit_PoW_addressCap(t *testing.T) {
	rlErr := &ratelimit.ErrRateLimitExceeded{RetryAfter: 60}
	g := New(powCfg(), nil, &stubPow{ok: true}, nil, nil, nil, &stubLimiter{checkAddressErr: rlErr})
	r := newHTTPReq(t)

	_, err := g.Admit(t.Context(), r, powReq(), defaultCC())
	var rl *ratelimit.ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}

// --- anonymous path ---

func TestAdmit_Anonymous_success(t *testing.T) {
	g := New(config.AbuseConfig{}, nil, nil, nil, nil, nil, &stubLimiter{})
	r := newHTTPReq(t)

	d, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if d.Mechanism != MechanismAnonymous {
		t.Errorf("mechanism: got %q, want %q", d.Mechanism, MechanismAnonymous)
	}
}

func TestAdmit_Anonymous_addressCap(t *testing.T) {
	rlErr := &ratelimit.ErrRateLimitExceeded{RetryAfter: 60}
	g := New(config.AbuseConfig{}, nil, nil, nil, nil, nil, &stubLimiter{checkAddressErr: rlErr})
	r := newHTTPReq(t)

	_, err := g.Admit(t.Context(), r, newReq(), defaultCC())
	var rl *ratelimit.ErrRateLimitExceeded
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}
