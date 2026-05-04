package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// testRouter builds a minimal chi router with the chain detail route wired up.
// Required for tests that need chi URL params extracted (e.g. {chain_id}).
func (h *Handler) testRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/v1/chains/{chain_id}", h.ChainDetail)
	return r
}
