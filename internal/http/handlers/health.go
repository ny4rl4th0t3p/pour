package handlers

import (
	"net/http"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Health handles GET /health.
func (*Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, pourapi.HealthResponse{Status: "ok"})
}
