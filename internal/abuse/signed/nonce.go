package signed

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const nonceBytes = 16 // 32-char hex string; 128 bits of entropy

// NonceStore issues and validates single-use nonces for the signed-challenge mechanism.
// Expired entries are swept lazily on Issue.
type NonceStore struct {
	mu     sync.Mutex
	nonces map[string]time.Time
	ttl    time.Duration
}

// NewNonceStore creates a NonceStore whose nonces expire after ttl.
func NewNonceStore(ttl time.Duration) *NonceStore {
	return &NonceStore{
		nonces: make(map[string]time.Time),
		ttl:    ttl,
	}
}

// Issue generates a fresh random nonce, stores it with an expiry, and returns it.
// Expired nonces are swept before the new entry is inserted.
func (s *NonceStore) Issue() (string, error) {
	b := make([]byte, nonceBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signed: generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.nonces[nonce] = time.Now().Add(s.ttl)
	return nonce, nil
}

// Consume checks that nonce is known and unexpired, then deletes it (single-use).
// Returns false when the nonce is unknown or expired.
func (s *NonceStore) Consume(nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.nonces[nonce]
	delete(s.nonces, nonce)
	return ok && time.Now().Before(expiry)
}

// sweep removes all expired entries. Must be called with s.mu held.
func (s *NonceStore) sweep() {
	now := time.Now()
	for k, exp := range s.nonces {
		if now.After(exp) {
			delete(s.nonces, k)
		}
	}
}
