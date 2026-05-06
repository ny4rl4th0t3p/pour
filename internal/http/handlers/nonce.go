package handlers

import (
	"net/http"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// SignNonce handles GET /v1/sign/nonce.
func (h *Handler) SignNonce(w http.ResponseWriter, _ *http.Request) {
	if !h.abuseCfg.SignatureChallenge.Enabled {
		writeError(w, http.StatusNotFound, "signed challenge not enabled")
		return
	}
	nonce, err := h.nonceIssuer.Issue()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue nonce")
		return
	}
	writeJSON(w, http.StatusOK, pourapi.NonceResponse{Nonce: nonce})
}
