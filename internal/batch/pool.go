package batch

import (
	"context"
	"sync"
	"time"
)

// Status represents the operational state of a Distributor.
type Status int

const (
	StatusHealthy Status = iota
	StatusRecovering
)

// Distributor is one signing account with its own batch window.
type Distributor struct {
	KeyIndex uint32
	Window   *Window
	status   Status
}

// Pool manages a set of Distributors and routes incoming requests to the
// least-loaded healthy one.
type Pool struct {
	distributors []*Distributor
	mu           sync.RWMutex
}

// NewPool creates a pool of n distributors (key indices 1..N), each with its
// own Window. flushFn is called with the key index and the drained batch.
func NewPool(n int, windowDuration time.Duration, maxRecipients, maxQueueDepth int,
	flushFn func(keyIndex uint32, batch []Request)) *Pool {
	distributors := make([]*Distributor, n)
	for i := range distributors {
		keyIndex := uint32(i + 1)
		idx := keyIndex // capture for closure
		distributors[i] = &Distributor{
			KeyIndex: idx,
			Window: NewWindow(windowDuration, maxRecipients, maxQueueDepth, func(batch []Request) {
				flushFn(idx, batch)
			}),
			status: StatusHealthy,
		}
	}
	return &Pool{distributors: distributors}
}

// Route enqueues req on the healthy distributor with the lowest queue depth.
// Returns ErrNoHealthyDistributor if all distributors are in recovery.
// Returns ErrAllFull if all healthy queues are at capacity.
func (p *Pool) Route(req Request) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var best *Distributor
	for _, d := range p.distributors {
		if d.status != StatusHealthy {
			continue
		}
		if best == nil || d.Window.Depth() < best.Window.Depth() {
			best = d
		}
	}
	if best == nil {
		return ErrNoHealthyDistributor
	}
	if err := best.Window.Enqueue(req); err != nil {
		// best had the minimum depth; if it's full, all healthy queues are full.
		return ErrAllFull
	}
	return nil
}

// MarkRecovering sets the distributor with the given key index to StatusRecovering.
func (p *Pool) MarkRecovering(keyIndex uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range p.distributors {
		if d.KeyIndex == keyIndex {
			d.status = StatusRecovering
			return
		}
	}
}

// MarkHealthy sets the distributor with the given key index to StatusHealthy.
func (p *Pool) MarkHealthy(keyIndex uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range p.distributors {
		if d.KeyIndex == keyIndex {
			d.status = StatusHealthy
			return
		}
	}
}

// Healthy returns a snapshot of all healthy distributors.
func (p *Pool) Healthy() []*Distributor {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []*Distributor
	for _, d := range p.distributors {
		if d.status == StatusHealthy {
			out = append(out, d)
		}
	}
	return out
}

// All returns a snapshot of all distributors regardless of status.
func (p *Pool) All() []*Distributor {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Distributor, len(p.distributors))
	copy(out, p.distributors)
	return out
}

// Start launches all distributor window goroutines.
func (p *Pool) Start(ctx context.Context) {
	for _, d := range p.distributors {
		d.Window.Start(ctx)
	}
}
