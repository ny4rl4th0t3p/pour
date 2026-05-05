package admin

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// Deps holds dependencies for the admin handler.
type Deps struct {
	RegStore   *chainregistry.Store
	Manager    *chain.Manager
	GasCache   *gascache.Cache
	ConfigPath string
	Logger     *slog.Logger
}

// Handler exposes admin endpoints as a chi sub-router.
type Handler struct {
	regStore   *chainregistry.Store
	manager    *chain.Manager
	gasCache   *gascache.Cache
	configPath string
	log        *slog.Logger
}

// New creates a Handler from Deps.
func New(deps Deps) *Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Handler{
		regStore:   deps.RegStore,
		manager:    deps.Manager,
		gasCache:   deps.GasCache,
		configPath: deps.ConfigPath,
		log:        deps.Logger,
	}
}

// Router returns an http.Handler with all admin routes registered.
// Mount at /admin in the main router.
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/registry/snapshot", h.Snapshot)
	r.Get("/registry/pending", h.Pending)
	r.Post("/registry/accept", h.Accept)
	r.Post("/registry/refresh", h.Refresh)
	r.Post("/reload", h.Reload)
	r.Get("/distributors/{chain}", h.DistributorList)
	r.Post("/distributors/{chain}/refill", h.DistributorRefill)
	r.Get("/chains/{chain}/gas-cache", h.GasCacheGet)
	r.Post("/chains/{chain}/gas-cache/reset", h.GasCacheReset)
	r.Get("/chains/{chain}/status", h.ChainStatusGet)
	r.Post("/chains/{chain}/resume", h.ChainResume)
	return r
}

// Snapshot handles GET /admin/registry/snapshot.
// Returns the full resolved view of all chains in the registry store.
func (h *Handler) Snapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.regStore.List())
}

// Pending handles GET /admin/registry/pending.
// Returns all freeze-policy changes awaiting operator acceptance.
func (h *Handler) Pending(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.regStore.Pending())
}

type acceptRequest struct {
	ChainID string `json:"chain_id"`
	Field   string `json:"field"` // empty = accept all pending fields for this chain
}

// Accept handles POST /admin/registry/accept.
// Body: {"chain_id": "...", "field": "..."} to accept one field, or
//
//	{"chain_id": "..."} to accept all pending fields for that chain.
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	var req acceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.ChainID == "" {
		writeError(w, http.StatusBadRequest, "chain_id is required")
		return
	}

	if req.Field != "" {
		if err := h.regStore.Accept(req.ChainID, req.Field); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, chainregistry.ErrPendingChange) || errors.Is(err, chainregistry.ErrChainNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	// Accept all pending fields for this chain.
	pending := h.regStore.Pending()
	accepted := 0
	for _, pc := range pending {
		if pc.ChainID != req.ChainID {
			continue
		}
		if err := h.regStore.Accept(pc.ChainID, pc.Field); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		accepted++
	}
	if accepted == 0 {
		writeError(w, http.StatusNotFound, "no pending changes for chain")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type refreshResponse struct {
	HotReloaded int `json:"hot_reloaded"`
	Warned      int `json:"warned"`
	Frozen      int `json:"frozen"`
	Removed     int `json:"removed"`
}

// Refresh handles POST /admin/registry/refresh.
// Triggers an immediate live fetch, applies it to the store, and reconciles connections.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	cs, err := h.manager.Refresh(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, refreshResponse{
		HotReloaded: len(cs.HotReloaded),
		Warned:      len(cs.Warned),
		Frozen:      len(cs.Frozen),
		Removed:     len(cs.Removed),
	})
}

// Reload handles POST /admin/reload.
// Re-reads chains.yml, updates registry overrides and drip policy, reconciles connections.
// Note: newly added registry chains not present at startup require a full restart.
func (h *Handler) Reload(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.LoadChains(h.configPath)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.manager.Reload(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log.Info("admin: config reloaded", "path", h.configPath)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type distributorJSON struct {
	Index      uint32 `json:"index"`
	Address    string `json:"address"`
	Balance    string `json:"balance"`
	QueueDepth int    `json:"queue_depth"`
	Status     string `json:"status"`
}

func distributorStatusString(s batch.Status) string {
	if s == batch.StatusRecovering {
		return "recovering"
	}
	return "healthy"
}

// DistributorList handles GET /admin/distributors/{chain}.
// Returns per-distributor state including live balance.
func (h *Handler) DistributorList(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain")
	c, ok := h.manager.GetChain(chainID)
	if !ok {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	states := c.DistributorStates(r.Context())
	out := make([]distributorJSON, len(states))
	for i, s := range states {
		out[i] = distributorJSON{
			Index:      s.KeyIndex,
			Address:    s.Address,
			Balance:    s.Balance,
			QueueDepth: s.QueueDepth,
			Status:     distributorStatusString(s.Status),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"distributors": out})
}

type refillRequest struct {
	Index *uint32 `json:"index"` // nil = all distributors
}

type refillResult struct {
	Index int    `json:"index"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// DistributorRefill handles POST /admin/distributors/{chain}/refill.
// Body: {"index": N} to refill a specific distributor, or {} to refill all below threshold.
func (h *Handler) DistributorRefill(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain")
	c, ok := h.manager.GetChain(chainID)
	if !ok {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	var req refillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Index != nil {
		err := c.RefillNow(r.Context(), *req.Index)
		res := refillResult{Index: int(*req.Index), OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": []refillResult{res}})
		return
	}
	states := c.DistributorStates(r.Context())
	results := make([]refillResult, len(states))
	for i, s := range states {
		err := c.RefillNow(r.Context(), s.KeyIndex)
		results[i] = refillResult{Index: int(s.KeyIndex), OK: err == nil}
		if err != nil {
			results[i].Error = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// GasCacheGet handles GET /admin/chains/{chain}/gas-cache.
func (h *Handler) GasCacheGet(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain")
	if _, ok := h.manager.GetChain(chainID); !ok {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	row, found, err := h.gasCache.Read(r.Context(), chainID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no gas cache entry for chain")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// GasCacheReset handles POST /admin/chains/{chain}/gas-cache/reset.
func (h *Handler) GasCacheReset(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain")
	if _, ok := h.manager.GetChain(chainID); !ok {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	if err := h.gasCache.Reset(r.Context(), chainID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type chainStatusJSON struct {
	Suspended           bool   `json:"suspended"`
	SuspendReason       string `json:"suspend_reason,omitempty"`
	MultiSendDisabled   bool   `json:"multisend_disabled"`
	SendFailStreak      int32  `json:"send_fail_streak"`
	MultiSendFailStreak int32  `json:"multisend_fail_streak"`
}

// ChainStatusGet handles GET /admin/chains/{chain}/status.
func (h *Handler) ChainStatusGet(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain")
	c, ok := h.manager.GetChain(chainID)
	if !ok {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	snap := c.ChainStatus()
	writeJSON(w, http.StatusOK, chainStatusJSON{
		Suspended:           snap.Suspended,
		SuspendReason:       snap.SuspendReason,
		MultiSendDisabled:   snap.MultiSendDisabled,
		SendFailStreak:      snap.SendFailStreak,
		MultiSendFailStreak: snap.MultiSendFailStreak,
	})
}

// ChainResume handles POST /admin/chains/{chain}/resume.
func (h *Handler) ChainResume(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain")
	c, ok := h.manager.GetChain(chainID)
	if !ok {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	if !c.ChainStatus().Suspended {
		writeError(w, http.StatusConflict, "chain is not suspended")
		return
	}
	c.Resume()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("admin: encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
