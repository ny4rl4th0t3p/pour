//go:build no_ui

package ui

import "net/http"

// Handler returns a 404 handler; the UI is excluded from this build.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}
