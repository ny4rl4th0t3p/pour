package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// Deps holds dependencies for the admin handler.
type Deps struct {
	RegStore   *chainregistry.Store
	Manager    *chain.Manager
	ConfigPath string
	Logger     *slog.Logger
}

// Handler exposes admin endpoints as a chi sub-router.
type Handler struct {
	regStore   *chainregistry.Store
	manager    *chain.Manager
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
