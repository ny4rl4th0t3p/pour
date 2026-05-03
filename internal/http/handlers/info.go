package handlers

import (
	"net/http"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Info handles GET /v1/info.
func (h *Handler) Info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, pourapi.InfoResponse{
		Version:          h.version,
		RegistryRevision: "",
		Abuse: pourapi.AbuseInfo{
			PoWEnabled:                false,
			APIKeysEnabled:            false,
			SignatureChallengeEnabled: false,
		},
	})
}
