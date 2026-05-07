//go:build !no_ui

package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html
var assets embed.FS

// Handler returns an http.Handler that serves the embedded faucet UI.
func Handler() http.Handler {
	return http.FileServer(http.FS(assets))
}
