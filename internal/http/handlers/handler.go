package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Broadcaster abstracts tx.Client for handler testability.
type Broadcaster interface {
	BuildAndBroadcast(ctx context.Context, req tx.SendRequest) (*tx.BroadcastResult, error)
}

// RateLimiter abstracts *ratelimit.Limiter for handler testability.
type RateLimiter interface {
	Check(ctx context.Context, ip, chainID string) error
}

// DripStore abstracts store.Store drip writes for handler testability.
type DripStore interface {
	RecordDrip(ctx context.Context, d store.DripRecord) (int64, error)
}

// Deps holds all dependencies for the handler set.
type Deps struct {
	Chains       map[string]config.ChainConfig // enabled chains keyed by chain_id
	Broadcasters map[string]Broadcaster
	Limiter      RateLimiter
	DripStore    DripStore
	GasCache     tx.GasCache // optional; may be nil
	Mnemonic     string
	Version      string
}

// Handler holds injected dependencies and exposes one method per REST endpoint.
type Handler struct {
	chains       map[string]config.ChainConfig
	broadcasters map[string]Broadcaster
	limiter      RateLimiter
	dripStore    DripStore
	gasCache     tx.GasCache
	mnemonic     string
	version      string
}

// New constructs a Handler from the provided Deps.
func New(deps Deps) *Handler {
	return &Handler{
		chains:       deps.Chains,
		broadcasters: deps.Broadcasters,
		limiter:      deps.Limiter,
		dripStore:    deps.DripStore,
		gasCache:     deps.GasCache,
		mnemonic:     deps.Mnemonic,
		version:      deps.Version,
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
