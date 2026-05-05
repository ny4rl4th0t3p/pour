package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ny4rl4th0t3p/pour/internal/chain"
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
	Source              chain.ChainSource
	RegistryRefreshMode string
	Broadcasters        map[string]Broadcaster
	Limiter             RateLimiter
	DripStore           DripStore
	Version             string
}

// Handler holds injected dependencies and exposes one method per REST endpoint.
type Handler struct {
	source              chain.ChainSource
	registryRefreshMode string
	broadcasters        map[string]Broadcaster
	limiter             RateLimiter
	dripStore           DripStore
	version             string
}

// New constructs a Handler from the provided Deps.
func New(deps Deps) *Handler {
	return &Handler{
		source:              deps.Source,
		registryRefreshMode: deps.RegistryRefreshMode,
		broadcasters:        deps.Broadcasters,
		limiter:             deps.Limiter,
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
