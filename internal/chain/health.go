package chain

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

const probeInterval = 30 * time.Second
const probeDialTimeout = 5 * time.Second

// startProbeLoop probes unhealthy endpoints every 30s and restores any that respond.
// Runs until ctx is canceled.
func startProbeLoop(ctx context.Context, pool *tx.EndpointPool, log *slog.Logger) {
	go func() {
		ticker := time.NewTicker(probeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, url := range pool.Unhealthy() {
					if probeEndpoint(ctx, url) {
						pool.MarkHealthy(url)
						log.Info("chain: endpoint restored", "url", url)
					}
				}
			}
		}
	}()
}

// probeEndpoint returns true if a TCP connection to url succeeds within the timeout.
func probeEndpoint(ctx context.Context, url string) bool {
	dialCtx, cancel := context.WithTimeout(ctx, probeDialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", url)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
