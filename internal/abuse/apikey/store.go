package apikey

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/ny4rl4th0t3p/pour/internal/store"
)

var (
	ErrNotFound      = errors.New("apikey: not found")
	ErrInvalidSecret = errors.New("apikey: invalid secret")
	ErrRevoked       = errors.New("apikey: revoked")
	ErrExpired       = errors.New("apikey: expired")
)

const (
	secretPrefix  = "pour_key_"
	secretIDHex   = 16 // hex chars encoding 8 ID bytes — embedded in the token for O(1) lookup
	secretBodyLen = 32 // random bytes forming the verifiable secret body
	saltLen       = 16
	argonTime     = 1
	argonMemory   = 64 * 1024 // 64 MiB
	argonThreads  = 4
	argonKeyLen   = 32
)

// CreateParams describes a new API key to issue.
type CreateParams struct {
	Label            string
	ChainScope       []string          // required; ["*"] = all chains
	PerChainDrips    map[string]string // chain_id → coin string; nil = inherit drip.anonymous
	RateLimitPerHour int               // 0 = inherit global ip_rate_limit
	ExpiresAt        *time.Time        // nil = never expires
}

// Key is an issued API key without its secret.
type Key struct {
	ID               string
	Label            string
	ChainScope       []string
	PerChainDrips    map[string]string
	RateLimitPerHour int
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
}

// DripsForChain returns the per-chain drip override or "" to inherit drip.anonymous.
func (k *Key) DripsForChain(chainID string) string {
	return k.PerChainDrips[chainID]
}

// InScope reports whether chainID is covered by the key's chain_scope.
func (k *Key) InScope(chainID string) bool {
	for _, s := range k.ChainScope {
		if s == "*" || s == chainID {
			return true
		}
	}
	return false
}

// Store manages API keys backed by the SQLite api_keys table.
type Store struct {
	db *sql.DB
}

// New creates a Store using the underlying database from s.
func New(s *store.Store) *Store {
	return &Store{db: s.DB()}
}

// Create issues a new key. Returns the DB id and the raw secret (shown once).
// Secret format: pour_key_<16-hex-id><43-base64url-body>
func (s *Store) Create(ctx context.Context, p CreateParams) (id, secret string, err error) {
	idRaw := make([]byte, 8)
	if _, err = rand.Read(idRaw); err != nil {
		return "", "", fmt.Errorf("apikey: generate id: %w", err)
	}
	idHex := hex.EncodeToString(idRaw)
	id = "key_" + idHex

	body := make([]byte, secretBodyLen)
	if _, err = rand.Read(body); err != nil {
		return "", "", fmt.Errorf("apikey: generate secret: %w", err)
	}
	secret = secretPrefix + idHex + base64.RawURLEncoding.EncodeToString(body)

	salt := make([]byte, saltLen)
	if _, err = rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("apikey: generate salt: %w", err)
	}
	hash := argon2.IDKey(body, salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	secretHash := append(salt, hash...) //nolint:gocritic // intentional: salt||hash blob

	scopeJSON, err := json.Marshal(p.ChainScope)
	if err != nil {
		return "", "", fmt.Errorf("apikey: marshal scope: %w", err)
	}

	var dripsArg any
	if p.PerChainDrips != nil {
		b, merr := json.Marshal(p.PerChainDrips)
		if merr != nil {
			return "", "", fmt.Errorf("apikey: marshal drips: %w", merr)
		}
		dripsArg = string(b)
	}

	var expiresArg any
	if p.ExpiresAt != nil {
		expiresArg = p.ExpiresAt.Unix()
	}

	var rlArg any
	if p.RateLimitPerHour > 0 {
		rlArg = p.RateLimitPerHour
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO api_keys
			(id, secret_hash, label, chain_scope, per_chain_drips, rate_limit_per_hour, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, secretHash, p.Label, string(scopeJSON), dripsArg, rlArg, expiresArg, time.Now().Unix())
	if err != nil {
		return "", "", fmt.Errorf("apikey: insert: %w", err)
	}
	return id, secret, nil
}

// List returns all non-revoked keys without their secrets.
func (s *Store) List(ctx context.Context) ([]*Key, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, label, chain_scope, per_chain_drips, rate_limit_per_hour,
		       expires_at, created_at, last_used_at
		FROM api_keys
		WHERE revoked_at IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("apikey: list: %w", err)
	}
	defer rows.Close()

	var keys []*Key
	for rows.Next() {
		k, serr := scanKey(rows)
		if serr != nil {
			return nil, serr
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Revoke sets revoked_at for the given key ID. Returns ErrNotFound if absent or already revoked.
func (s *Store) Revoke(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("apikey: revoke: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate verifies rawSecret and returns the key if valid, unexpired, and not revoked.
func (s *Store) Authenticate(ctx context.Context, rawSecret string) (*Key, error) {
	if !strings.HasPrefix(rawSecret, secretPrefix) {
		return nil, ErrInvalidSecret
	}
	tail := rawSecret[len(secretPrefix):]
	if len(tail) <= secretIDHex {
		return nil, ErrInvalidSecret
	}
	idHex := tail[:secretIDHex]
	encoded := tail[secretIDHex:]
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(body) != secretBodyLen {
		return nil, ErrInvalidSecret
	}
	// Reject non-canonical encodings: base64 ignores padding bits in the last
	// character, so a tampered token with the same significant bits but different
	// padding bits would otherwise decode to the same body and pass argon2.
	if base64.RawURLEncoding.EncodeToString(body) != encoded {
		return nil, ErrInvalidSecret
	}

	id := "key_" + idHex
	var (
		secretHash []byte
		scopeJSON  sql.NullString
		dripsJSON  sql.NullString
		rl         sql.NullInt64
		expiresAt  sql.NullInt64
		createdAt  int64
		lastUsedAt sql.NullInt64
		revokedAt  sql.NullInt64
		label      sql.NullString
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT secret_hash, label, chain_scope, per_chain_drips, rate_limit_per_hour,
		       expires_at, created_at, last_used_at, revoked_at
		FROM api_keys WHERE id = ?
	`, id).Scan(
		&secretHash, &label, &scopeJSON, &dripsJSON, &rl,
		&expiresAt, &createdAt, &lastUsedAt, &revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSecret
	}
	if err != nil {
		return nil, fmt.Errorf("apikey: query: %w", err)
	}

	if revokedAt.Valid {
		return nil, ErrRevoked
	}
	if expiresAt.Valid && time.Now().Unix() > expiresAt.Int64 {
		return nil, ErrExpired
	}

	if len(secretHash) != saltLen+argonKeyLen {
		return nil, fmt.Errorf("apikey: corrupt secret_hash for %s", id)
	}
	computed := argon2.IDKey(body, secretHash[:saltLen], argonTime, argonMemory, argonThreads, argonKeyLen)
	if subtle.ConstantTimeCompare(computed, secretHash[saltLen:]) != 1 {
		return nil, ErrInvalidSecret
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now().Unix(), id)

	k := &Key{
		ID:        id,
		Label:     label.String,
		CreatedAt: time.Unix(createdAt, 0),
	}
	if lastUsedAt.Valid {
		t := time.Unix(lastUsedAt.Int64, 0)
		k.LastUsedAt = &t
	}
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0)
		k.ExpiresAt = &t
	}
	if rl.Valid {
		k.RateLimitPerHour = int(rl.Int64)
	}
	if scopeJSON.Valid {
		if err := json.Unmarshal([]byte(scopeJSON.String), &k.ChainScope); err != nil {
			return nil, fmt.Errorf("apikey: unmarshal scope: %w", err)
		}
	}
	if dripsJSON.Valid {
		if err := json.Unmarshal([]byte(dripsJSON.String), &k.PerChainDrips); err != nil {
			return nil, fmt.Errorf("apikey: unmarshal drips: %w", err)
		}
	}
	return k, nil
}

// ParseBearer extracts a pour_key_* token from an Authorization: Bearer header.
// Returns ("", false) when the header is absent or not a pour_key_* token.
func ParseBearer(header string) (string, bool) {
	const bearer = "Bearer "
	if !strings.HasPrefix(header, bearer) {
		return "", false
	}
	token := strings.TrimPrefix(header, bearer)
	if !strings.HasPrefix(token, secretPrefix) {
		return "", false
	}
	return token, true
}

// scanKey reads a Key row from List queries (no secret_hash, no revoked_at column).
func scanKey(rows *sql.Rows) (*Key, error) {
	var (
		k          Key
		label      sql.NullString
		scopeJSON  sql.NullString
		dripsJSON  sql.NullString
		rl         sql.NullInt64
		expiresAt  sql.NullInt64
		createdAt  int64
		lastUsedAt sql.NullInt64
	)
	if err := rows.Scan(&k.ID, &label, &scopeJSON, &dripsJSON, &rl, &expiresAt, &createdAt, &lastUsedAt); err != nil {
		return nil, fmt.Errorf("apikey: scan: %w", err)
	}
	k.Label = label.String
	k.CreatedAt = time.Unix(createdAt, 0)
	if lastUsedAt.Valid {
		t := time.Unix(lastUsedAt.Int64, 0)
		k.LastUsedAt = &t
	}
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0)
		k.ExpiresAt = &t
	}
	if rl.Valid {
		k.RateLimitPerHour = int(rl.Int64)
	}
	if scopeJSON.Valid {
		if err := json.Unmarshal([]byte(scopeJSON.String), &k.ChainScope); err != nil {
			return nil, fmt.Errorf("apikey: unmarshal scope: %w", err)
		}
	}
	if dripsJSON.Valid {
		if err := json.Unmarshal([]byte(dripsJSON.String), &k.PerChainDrips); err != nil {
			return nil, fmt.Errorf("apikey: unmarshal drips: %w", err)
		}
	}
	return &k, nil
}
