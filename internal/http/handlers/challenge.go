package handlers

import (
	"net/http"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// PowChallenge handles GET /v1/pow/challenge.
func (h *Handler) PowChallenge(w http.ResponseWriter, _ *http.Request) {
	if !h.abuseCfg.PoW.Enabled {
		writeError(w, http.StatusNotFound, "PoW not enabled")
		return
	}
	challenge, err := h.powIssuer.NewChallenge()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue challenge")
		return
	}
	writeJSON(w, http.StatusOK, pourapi.ChallengeResponse{Challenge: challenge})
}
