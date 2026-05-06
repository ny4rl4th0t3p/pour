package handlers

import (
	"net/http"
	"time"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Info handles GET /v1/info.
func (h *Handler) Info(w http.ResponseWriter, _ *http.Request) {
	lastFetched := ""
	if t := h.source.LastFetched(); !t.IsZero() {
		lastFetched = t.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, pourapi.InfoResponse{
		Version:             h.version,
		RegistryLastFetched: lastFetched,
		RegistryRefreshMode: h.registryRefreshMode,
		PendingFrozenCount:  h.source.PendingFrozenCount(),
		Abuse: pourapi.AbuseInfo{
			PoWEnabled:                h.abuseCfg.PoW.Enabled,
			APIKeysEnabled:            h.abuseCfg.APIKeys.Enabled,
			SignatureChallengeEnabled: h.abuseCfg.SignatureChallenge.Enabled,
		},
	})
}
