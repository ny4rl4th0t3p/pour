package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	TokenEnvVar = "POUR_ADMIN_TOKEN"
	TokenFile   = ".pour-admin-token" //nolint:gosec // path, not a credential
	tokenPrefix = "pour_admin_"
	tokenBytes  = 32
)

// defaultAllowedCIDRs covers both IPv4 and IPv6 loopback for local dev setups.
var defaultAllowedCIDRs = []string{"127.0.0.1/32", "::1/128"}

// TokenStore holds the admin bearer token, loaded or generated at startup.
// The token may be rotated at runtime via Rotate; all methods are safe for concurrent use.
type TokenStore struct {
	mu    sync.RWMutex
	token string
}

// NewTokenStore resolves the admin token in priority order:
// .pour-admin-token file → POUR_ADMIN_TOKEN env → generate + write new token.
//
// The file takes precedence over the env var so that token rotations (which write
// a new file) survive process restarts without the env var winning back. To revert
// to an env-var-managed token, delete the file explicitly.
func NewTokenStore() (*TokenStore, error) {
	if data, err := os.ReadFile(TokenFile); err == nil {
		return &TokenStore{token: strings.TrimSpace(string(data))}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("admin: read token file: %w", err)
	}
	if t := os.Getenv(TokenEnvVar); t != "" {
		return &TokenStore{token: t}, nil
	}
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(TokenFile, []byte(token+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("admin: write token file: %w", err)
	}
	slog.Info("admin: generated admin token", "path", TokenFile)
	return &TokenStore{token: token}, nil
}

// Token returns the current admin bearer token.
func (ts *TokenStore) Token() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.token
}

// HMACKey returns a 32-byte key derived from the admin token for use in HMAC signing.
// The raw token is not exposed.
func (ts *TokenStore) HMACKey() []byte {
	raw := sha256.Sum256([]byte(ts.Token()))
	return raw[:]
}

// Rotate generates a new admin token, overwrites TokenFile, and replaces the in-memory
// token atomically. Returns the new token (shown once; caller must log or return it).
// After rotation, the file takes priority over POUR_ADMIN_TOKEN on the next restart.
func (ts *TokenStore) Rotate() (string, error) {
	newToken, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(TokenFile, []byte(newToken+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("admin: write token file: %w", err)
	}
	ts.mu.Lock()
	ts.token = newToken
	ts.mu.Unlock()
	return newToken, nil
}

// generateToken produces a new cryptographically random admin bearer token.
func generateToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("admin: generate token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// Middleware enforces IP allowlist then Bearer token authentication.
// allowedCIDRs defaults to ["127.0.0.1/32", "::1/128"] when empty.
// Returns 403 for disallowed IPs and 401 for missing or invalid tokens.
func Middleware(ts *TokenStore, allowedCIDRs []string) func(http.Handler) http.Handler {
	if len(allowedCIDRs) == 0 {
		allowedCIDRs = defaultAllowedCIDRs
	}
	nets := parseCIDRs(allowedCIDRs)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !ipAllowed(extractIP(r.RemoteAddr), nets) {
				writeError(w, http.StatusForbidden, "IP not allowed")
				return
			}
			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if bearer == "" || bearer != ts.Token() {
				writeError(w, http.StatusUnauthorized, "invalid or missing token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func ipAllowed(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
