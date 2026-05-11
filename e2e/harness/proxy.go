package harness

import (
	"io"
	"net"
	"sync"
	"testing"
)

// TCPProxy is a transparent TCP forwarder used in e2e tests to simulate endpoint
// failure: kill the proxy (Close) while the target remains up, then verify that
// the client falls over to an alternative endpoint.
type TCPProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	conns    []net.Conn
	closed   bool
}

// StartTCPProxy starts a TCP proxy on a random local port that forwards all
// traffic to target. Cleanup closes the proxy.
func StartTCPProxy(t *testing.T, target string) *TCPProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartTCPProxy: listen: %v", err)
	}
	p := &TCPProxy{listener: l, target: target}
	go p.serve()
	t.Cleanup(p.Close)
	return p
}

// Addr returns the proxy's listen address (e.g. "127.0.0.1:54321").
func (p *TCPProxy) Addr() string {
	return p.listener.Addr().String()
}

// Close stops the proxy and terminates all active forwarded connections so that
// clients using the proxy receive an immediate network error.
func (p *TCPProxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	_ = p.listener.Close()
	for _, c := range p.conns {
		_ = c.Close()
	}
}

func (p *TCPProxy) serve() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		go p.forward(client)
	}
}

func (p *TCPProxy) forward(client net.Conn) {
	defer client.Close()

	backend, err := net.Dial("tcp", p.target)
	if err != nil {
		return
	}
	defer backend.Close()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.conns = append(p.conns, client, backend)
	p.mu.Unlock()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(backend, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, backend); done <- struct{}{} }()
	<-done
}
