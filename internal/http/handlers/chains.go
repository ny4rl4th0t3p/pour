package handlers

import (
	"net/http"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Chains handles GET /v1/chains.
func (h *Handler) Chains(w http.ResponseWriter, _ *http.Request) {
	chains := make([]pourapi.ChainInfo, 0, len(h.chains))
	for id := range h.chains {
		c := h.chains[id]
		chains = append(chains, pourapi.ChainInfo{
			ChainID:      c.ChainID,
			Bech32Prefix: c.Bech32Prefix,
			DripAmount:   c.Drip.Anonymous,
		})
	}
	writeJSON(w, http.StatusOK, pourapi.ChainsResponse{Chains: chains})
}
