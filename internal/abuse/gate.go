package abuse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/apikey"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Mechanism identifies which abuse-prevention path admitted a pour request.
type Mechanism string

const (
	MechanismAPIKey    Mechanism = "api_key"
	MechanismSigned    Mechanism = "signed"
	MechanismPoW       Mechanism = "pow"
	MechanismAnonymous Mechanism = "anonymous"
)

// Decision is returned by Gate.Admit on success and carries everything the
// pour handler needs: which mechanism admitted the request, the resolved drip
// amount, and (when applicable) the API key ID or signer address.
type Decision struct {
	Mechanism     Mechanism
	DripCoin      tx.Coin
	APIKeyID      string
	SignerAddress string
}

// ChainContext holds per-chain values the HTTP handler resolves before calling
// Admit. This keeps Gate free of a chain.ChainSource dependency.
type ChainContext struct {
	ChainID       string
	KeyAlgo       string // "secp256k1" | "ethsecp256k1"
	DripAnonymous tx.Coin
	DripSigned    tx.Coin // zero Amount when drip.signed is not configured
	MaxPerDay     tx.Coin
}

// Sentinel errors returned by Admit. Callers map these to HTTP status codes.
var (
	ErrUnauthenticated = errors.New("gate: invalid or expired API key")
	ErrForbidden       = errors.New("gate: key not scoped to this chain")
	ErrPoWRequired     = errors.New("gate: PoW solution required")
	ErrBadPoW          = errors.New("gate: invalid PoW solution")
	ErrNonceRequired   = errors.New("gate: nonce required for signed mechanism")
	ErrBadNonce        = errors.New("gate: invalid or expired nonce")
	ErrBadSignature    = errors.New("gate: invalid signature")
	ErrPredicateFailed = errors.New("gate: predicate check failed")
)

// APIKeyAuthenticator authenticates raw API key secrets.
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, rawSecret string) (*apikey.Key, error)
}

// PowVerifier verifies Altcha PoW challenge+solution payloads.
type PowVerifier interface {
	Verify(challenge, solution string) (bool, error)
}

// NonceConsumer validates and single-use-consumes a signed-mechanism nonce.
type NonceConsumer interface {
	Consume(nonce string) bool
}

// SigVerifier verifies an ADR-036 signature over a nonce.
type SigVerifier interface {
	Verify(address, pubkeyB64, sigB64, nonce string) (bool, error)
}

// PredicateRunner evaluates the configured on-chain predicate for a signer.
type PredicateRunner interface {
	Check(ctx context.Context, predicate, targetChainID, signerAddress, minAmount string) error
}

// Limiter enforces IP, address-cap, and API-key rate limits.
type Limiter interface {
	Check(ctx context.Context, ip, chainID string) error
	CheckAddress(ctx context.Context, bech32Addr, keyAlgo, chainID string, dripCoin, maxPerDay tx.Coin) error
	CheckAPIKey(ctx context.Context, keyID, chainID string, rateLimitPerHour int) error
}

// Gate implements the §6.5 priority composition rule: api_key → signed → pow → anonymous.
type Gate struct {
	abuseCfg   config.AbuseConfig
	apiKeys    APIKeyAuthenticator
	pow        PowVerifier
	nonceStore NonceConsumer
	sigVerify  SigVerifier
	predicates PredicateRunner
	limiter    Limiter
}

// New creates a Gate. When a mechanism is disabled its corresponding dependency
// may be nil — Admit will never invoke it.
func New(
	cfg config.AbuseConfig,
	apiKeys APIKeyAuthenticator,
	pow PowVerifier,
	nonceStore NonceConsumer,
	sig SigVerifier,
	predicates PredicateRunner,
	limiter Limiter,
) *Gate {
	return &Gate{
		abuseCfg:   cfg,
		apiKeys:    apiKeys,
		pow:        pow,
		nonceStore: nonceStore,
		sigVerify:  sig,
		predicates: predicates,
		limiter:    limiter,
	}
}

// Admit evaluates all abuse controls for the incoming pour request and returns a
// Decision on success or a sentinel error the caller maps to an HTTP status code.
func (g *Gate) Admit(ctx context.Context, r *http.Request, req *pourapi.PourRequest, cc ChainContext) (*Decision, error) {
	ip := extractIP(r.RemoteAddr)
	if err := g.limiter.Check(ctx, ip, cc.ChainID); err != nil {
		return nil, err
	}

	if bearer, ok := apikey.ParseBearer(r.Header.Get("Authorization")); ok && g.abuseCfg.APIKeys.Enabled {
		return g.admitAPIKey(ctx, req, cc, bearer)
	}

	if req.Signature != nil && g.abuseCfg.SignatureChallenge.Enabled {
		return g.admitSigned(ctx, req, cc)
	}

	if g.abuseCfg.PoW.Enabled {
		return g.admitPoW(ctx, req, cc)
	}

	return g.admitAnonymous(ctx, req, cc)
}

func (g *Gate) admitAPIKey(ctx context.Context, req *pourapi.PourRequest, cc ChainContext, bearer string) (*Decision, error) {
	key, err := g.apiKeys.Authenticate(ctx, bearer)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if !key.InScope(cc.ChainID) {
		return nil, ErrForbidden
	}
	if err := g.limiter.CheckAPIKey(ctx, key.ID, cc.ChainID, key.RateLimitPerHour); err != nil {
		return nil, err
	}
	dripCoin := cc.DripAnonymous
	if dripStr := key.DripsForChain(cc.ChainID); dripStr != "" {
		if parsed, parseErr := config.ParseCoin(dripStr); parseErr == nil {
			dripCoin = parsed
		}
	}
	if err := g.limiter.CheckAddress(ctx, req.Address, cc.KeyAlgo, cc.ChainID, dripCoin, cc.MaxPerDay); err != nil {
		return nil, err
	}
	return &Decision{Mechanism: MechanismAPIKey, DripCoin: dripCoin, APIKeyID: key.ID}, nil
}

func (g *Gate) admitSigned(ctx context.Context, req *pourapi.PourRequest, cc ChainContext) (*Decision, error) {
	if req.Signature.Nonce == "" {
		return nil, ErrNonceRequired
	}
	if !g.nonceStore.Consume(req.Signature.Nonce) {
		return nil, ErrBadNonce
	}
	ok, err := g.sigVerify.Verify(req.Signature.Address, req.Signature.Pubkey, req.Signature.Signature, req.Signature.Nonce)
	if err != nil {
		return nil, fmt.Errorf("gate: verify sig: %w", err)
	}
	if !ok {
		return nil, ErrBadSignature
	}
	predChainID := g.abuseCfg.SignatureChallenge.PredicateChainID
	if predChainID == "" {
		predChainID = cc.ChainID
	}
	if err := g.predicates.Check(
		ctx,
		g.abuseCfg.SignatureChallenge.RequirePredicate,
		predChainID,
		req.Signature.Address,
		g.abuseCfg.SignatureChallenge.PredicateMinAmount,
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPredicateFailed, err)
	}
	dripCoin := cc.DripSigned
	if dripCoin.Amount == "" {
		slog.WarnContext(ctx, "drip.signed not configured, falling back to anonymous drip", "chain_id", cc.ChainID)
		dripCoin = cc.DripAnonymous
	}
	if err := g.limiter.CheckAddress(ctx, req.Address, cc.KeyAlgo, cc.ChainID, dripCoin, cc.MaxPerDay); err != nil {
		return nil, err
	}
	return &Decision{Mechanism: MechanismSigned, DripCoin: dripCoin, SignerAddress: req.Signature.Address}, nil
}

func (g *Gate) admitPoW(ctx context.Context, req *pourapi.PourRequest, cc ChainContext) (*Decision, error) {
	if req.Pow == nil {
		return nil, ErrPoWRequired
	}
	ok, err := g.pow.Verify(req.Pow.Challenge, req.Pow.Solution)
	if err != nil {
		return nil, fmt.Errorf("gate: verify pow: %w", err)
	}
	if !ok {
		return nil, ErrBadPoW
	}
	if err := g.limiter.CheckAddress(ctx, req.Address, cc.KeyAlgo, cc.ChainID, cc.DripAnonymous, cc.MaxPerDay); err != nil {
		return nil, err
	}
	return &Decision{Mechanism: MechanismPoW, DripCoin: cc.DripAnonymous}, nil
}

func (g *Gate) admitAnonymous(ctx context.Context, req *pourapi.PourRequest, cc ChainContext) (*Decision, error) {
	if err := g.limiter.CheckAddress(ctx, req.Address, cc.KeyAlgo, cc.ChainID, cc.DripAnonymous, cc.MaxPerDay); err != nil {
		return nil, err
	}
	return &Decision{Mechanism: MechanismAnonymous, DripCoin: cc.DripAnonymous}, nil
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
