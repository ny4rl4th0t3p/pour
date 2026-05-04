package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// ----- auth middleware tests -----

func newTokenStore() *TokenStore { return &TokenStore{token: "secret"} }

func newMWRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
}

func TestMiddleware_missingToken(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: got %d, want 401", w.Code)
	}
}

func TestMiddleware_wrongToken(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", w.Code)
	}
}

func TestMiddleware_forbiddenIP(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "10.0.0.1:1234" // not loopback
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("forbidden IP: got %d, want 403", w.Code)
	}
}

func TestMiddleware_allowed(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("allowed request: got %d, want 200", w.Code)
	}
}

func TestMiddleware_ipv6Loopback(t *testing.T) {
	mw := Middleware(newTokenStore(), nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := newMWRequest(t)
	r.RemoteAddr = "[::1]:1234"
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("IPv6 loopback: got %d, want 200", w.Code)
	}
}

// ----- handler tests -----

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	regStore, err := chainregistry.New(chainregistry.Options{})
	if err != nil {
		t.Fatalf("chainregistry.New: %v", err)
	}
	return New(Deps{RegStore: regStore})
}

func TestHandler_snapshot_empty(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/registry/snapshot", http.NoBody)
	w := httptest.NewRecorder()
	h.Snapshot(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var chains []*chainregistry.ChainInfo
	if err := json.NewDecoder(w.Body).Decode(&chains); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(chains) != 0 {
		t.Errorf("expected empty snapshot, got %d chains", len(chains))
	}
}

func TestHandler_pending_empty(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/registry/pending", http.NoBody)
	w := httptest.NewRecorder()
	h.Pending(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var pending []*chainregistry.PendingChange
	if err := json.NewDecoder(w.Body).Decode(&pending); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected empty pending, got %d", len(pending))
	}
}

func TestHandler_accept_badBody(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader("not json"))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body: got %d, want 400", w.Code)
	}
}

func TestHandler_accept_missingChainID(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader(`{"field":"Bech32Prefix"}`))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing chain_id: got %d, want 400", w.Code)
	}
}

func TestHandler_accept_notFound(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader(`{"chain_id":"osmosis-1","field":"Bech32Prefix"}`))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("not found: got %d, want 404", w.Code)
	}
}

func TestHandler_accept_allFields_notFound(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/registry/accept",
		strings.NewReader(`{"chain_id":"osmosis-1"}`))
	w := httptest.NewRecorder()
	h.Accept(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("all fields not found: got %d, want 404", w.Code)
	}
}
