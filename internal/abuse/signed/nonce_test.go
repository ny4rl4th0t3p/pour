package signed

import (
	"testing"
	"time"
)

func TestNonceStore_issueConsume(t *testing.T) {
	s := NewNonceStore(time.Minute)
	nonce, err := s.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if nonce == "" {
		t.Fatal("Issue returned empty nonce")
	}
	if !s.Consume(nonce) {
		t.Error("Consume should return true for a valid nonce")
	}
}

func TestNonceStore_singleUse(t *testing.T) {
	s := NewNonceStore(time.Minute)
	nonce, err := s.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	s.Consume(nonce)
	if s.Consume(nonce) {
		t.Error("second Consume should return false (single-use)")
	}
}

func TestNonceStore_expired(t *testing.T) {
	s := NewNonceStore(time.Millisecond)
	nonce, err := s.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Insert directly with a past expiry to avoid a real sleep.
	s.mu.Lock()
	s.nonces[nonce] = time.Now().Add(-time.Second)
	s.mu.Unlock()

	if s.Consume(nonce) {
		t.Error("Consume should return false for an expired nonce")
	}
}

func TestNonceStore_unknown(t *testing.T) {
	s := NewNonceStore(time.Minute)
	if s.Consume("doesnotexist") {
		t.Error("Consume should return false for an unknown nonce")
	}
}

func TestNonceStore_sweepOnIssue(t *testing.T) {
	s := NewNonceStore(time.Minute)

	// Insert an expired entry directly.
	s.mu.Lock()
	s.nonces["stale"] = time.Now().Add(-time.Second)
	s.mu.Unlock()

	if _, err := s.Issue(); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	s.mu.Lock()
	_, stillPresent := s.nonces["stale"]
	s.mu.Unlock()
	if stillPresent {
		t.Error("sweep on Issue should have removed expired entry")
	}
}
