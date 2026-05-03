package http

import (
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/http/handlers"
	ourmw "github.com/ny4rl4th0t3p/pour/internal/http/middleware"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/internal/ui"
)

// Deps holds everything cmd/pour passes into the HTTP layer.
type Deps struct {
	ChainsConfig *config.ChainsConfig
	Serve        *config.ServeConfig
	Store        *store.Store
	Limiter      handlers.RateLimiter
	Broadcasters map[string]handlers.Broadcaster // chain_id → tx.Client
	GasCache     tx.GasCache                     // optional; may be nil
	Mnemonic     string
	Version      string
}

// Server owns the chi router and listen address.
type Server struct {
	router nethttp.Handler
	addr   string
}

// New builds the chi router with all middleware and routes wired up.
func New(deps Deps) *Server {
	chains := enabledChains(deps.ChainsConfig)

	h := handlers.New(handlers.Deps{
		Chains:       chains,
		Broadcasters: deps.Broadcasters,
		Limiter:      deps.Limiter,
		DripStore:    deps.Store,
		GasCache:     deps.GasCache,
		Mnemonic:     deps.Mnemonic,
		Version:      deps.Version,
	})

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(ourmw.Logger)

	r.Post("/v1/pour", h.Pour)
	r.Get("/v1/info", h.Info)
	r.Get("/v1/chains", h.Chains)
	r.Get("/health", h.Health)

	if deps.Serve.Metrics {
		r.Get("/metrics", promhttp.Handler().ServeHTTP)
	}

	uiH := ui.Handler()
	r.Get("/", uiH.ServeHTTP)
	r.Get("/altcha.min.js", uiH.ServeHTTP)

	return &Server{router: r, addr: deps.Serve.Listen}
}

// Start begins serving on the configured address and blocks until ctx is canceled,
// then performs a graceful shutdown with a 5-second deadline.
func (s *Server) Start(ctx context.Context) error {
	srv := &nethttp.Server{
		Addr:              s.addr,
		Handler:           s.router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("http: shutdown: %w", err)
		}
		return nil
	}
}

// enabledChains returns a map of chain_id → ChainConfig for all enabled chains.
func enabledChains(cfg *config.ChainsConfig) map[string]config.ChainConfig {
	out := make(map[string]config.ChainConfig, len(cfg.Chains))
	for i := range cfg.Chains {
		c := cfg.Chains[i]
		if c.Enabled {
			out[c.ChainID] = c
		}
	}
	return out
}
