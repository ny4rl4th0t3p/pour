package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ny4rl4th0t3p/pour/internal/abuse"
	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Broadcaster abstracts tx.Client for handler testability.
type Broadcaster interface {
	BuildAndBroadcast(ctx context.Context, req tx.SendRequest) (*tx.BroadcastResult, error)
}

// ChainPourer abstracts chain.Manager for routing requests to the batch pool.
type ChainPourer interface {
	Pour(chainID string, req batch.Request) error
}

// Admitter abstracts *abuse.Gate for handler testability.
type Admitter interface {
	Admit(ctx context.Context, r *http.Request, req *pourapi.PourRequest, cc abuse.ChainContext) (*abuse.Decision, error)
}

// PowIssuer issues Altcha PoW challenges.
type PowIssuer interface {
	NewChallenge() (string, error)
}

// NonceIssuer issues single-use signed-mechanism nonces.
type NonceIssuer interface {
	Issue() (string, error)
}

// DripStore abstracts store.Store drip writes for handler testability.
type DripStore interface {
	RecordDrip(ctx context.Context, d store.DripRecord) (int64, error)
	UpdateDrip(ctx context.Context, id int64, status, txHash string, completedAt int64) error
}

// Deps holds all dependencies for the handler set.
type Deps struct {
	Source              chain.ChainSource
	RegistryRefreshMode string
	Pourer              ChainPourer
	Broadcasters        map[string]Broadcaster
	Gate                Admitter
	PowIssuer           PowIssuer
	NonceIssuer         NonceIssuer
	AbuseCfg            config.AbuseConfig
	DripStore           DripStore
	Version             string
}

// Handler holds injected dependencies and exposes one method per REST endpoint.
type Handler struct {
	source              chain.ChainSource
	registryRefreshMode string
	pourer              ChainPourer
	broadcasters        map[string]Broadcaster
	gate                Admitter
	powIssuer           PowIssuer
	nonceIssuer         NonceIssuer
	abuseCfg            config.AbuseConfig
	dripStore           DripStore
	version             string
}

// New constructs a Handler from the provided Deps.
func New(deps Deps) *Handler {
	return &Handler{
		source:              deps.Source,
		registryRefreshMode: deps.RegistryRefreshMode,
		pourer:              deps.Pourer,
		broadcasters:        deps.Broadcasters,
		gate:                deps.Gate,
		powIssuer:           deps.PowIssuer,
		nonceIssuer:         deps.NonceIssuer,
		abuseCfg:            deps.AbuseCfg,
		dripStore:           deps.DripStore,
		version:             deps.Version,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON: encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, pourapi.ErrorResponse{Error: msg})
}
