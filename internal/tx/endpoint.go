package tx

import (
	"strings"
	"sync"
)

// EndpointPool manages an ordered list of gRPC endpoint URLs with per-entry health tracking.
// tx.Client uses it to fail over to the next healthy endpoint on codes.Unavailable errors.
type EndpointPool struct {
	mu        sync.Mutex
	endpoints []endpointEntry
}

type endpointEntry struct {
	url     string
	healthy bool
}

// NewEndpointPool creates a pool from the given URLs; all start healthy.
func NewEndpointPool(urls []string) *EndpointPool {
	entries := make([]endpointEntry, len(urls))
	for i, u := range urls {
		entries[i] = endpointEntry{url: u, healthy: true}
	}
	return &EndpointPool{endpoints: entries}
}

// Next returns the first healthy endpoint URL. Returns ("", false) if none are healthy.
func (p *EndpointPool) Next() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.endpoints {
		if e.healthy {
			return e.url, true
		}
	}
	return "", false
}

// MarkUnhealthy marks the endpoint with the given URL as unhealthy.
func (p *EndpointPool) MarkUnhealthy(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.endpoints {
		if p.endpoints[i].url == url {
			p.endpoints[i].healthy = false
			return
		}
	}
}

// MarkHealthy marks the endpoint with the given URL as healthy.
func (p *EndpointPool) MarkHealthy(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.endpoints {
		if p.endpoints[i].url == url {
			p.endpoints[i].healthy = true
			return
		}
	}
}

// Unhealthy returns a snapshot of all unhealthy endpoint URLs, for use by probe goroutines.
func (p *EndpointPool) Unhealthy() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.endpoints {
		if !e.healthy {
			out = append(out, e.url)
		}
	}
	return out
}

// endpointIsTLS returns true when the URL implies TLS (port :443).
func endpointIsTLS(url string) bool {
	return strings.HasSuffix(url, ":443")
}
