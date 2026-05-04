package admin

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
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
type TokenStore struct {
	token string
}

// NewTokenStore resolves the admin token in priority order:
// POUR_ADMIN_TOKEN env → .pour-admin-token file → generate + write new token.
func NewTokenStore() (*TokenStore, error) {
	if t := os.Getenv(TokenEnvVar); t != "" {
		return &TokenStore{token: t}, nil
	}
	if data, err := os.ReadFile(TokenFile); err == nil {
		return &TokenStore{token: strings.TrimSpace(string(data))}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("admin: read token file: %w", err)
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("admin: generate token: %w", err)
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	if err := os.WriteFile(TokenFile, []byte(token+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("admin: write token file: %w", err)
	}
	slog.Info("admin: generated admin token", "path", TokenFile)
	return &TokenStore{token: token}, nil
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
			if bearer == "" || bearer != ts.token {
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
