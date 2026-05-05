package batch

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func noopFlush(_ uint32, _ []Request) {}

func TestPool_RouteLeastLoaded(t *testing.T) {
	p := NewPool(2, 10*time.Second, 100, 500, noopFlush)

	// Fill distributor 1 (key index 1) with 2 requests without flushing.
	for range 2 {
		if err := p.distributors[0].Window.Enqueue(makeRequest("a")); err != nil {
			t.Fatalf("pre-fill enqueue: %v", err)
		}
	}

	// Route should pick distributor 2 (key index 2, depth 0).
	if err := p.Route(makeRequest("b")); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d := p.distributors[1].Window.Depth(); d != 1 {
		t.Errorf("distributor 2 depth = %d, want 1", d)
	}
	if d := p.distributors[0].Window.Depth(); d != 2 {
		t.Errorf("distributor 1 depth = %d, want 2 (unchanged)", d)
	}
}

func TestPool_RouteAroundRecovering(t *testing.T) {
	p := NewPool(2, 10*time.Second, 100, 500, noopFlush)
	p.MarkRecovering(1)

	if err := p.Route(makeRequest("x")); err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d := p.distributors[1].Window.Depth(); d != 1 {
		t.Errorf("distributor 2 depth = %d, want 1", d)
	}
	if d := p.distributors[0].Window.Depth(); d != 0 {
		t.Errorf("distributor 1 depth = %d, want 0 (recovering, skipped)", d)
	}
}

func TestPool_ErrNoHealthyDistributor(t *testing.T) {
	p := NewPool(2, 10*time.Second, 100, 500, noopFlush)
	p.MarkRecovering(1)
	p.MarkRecovering(2)

	if err := p.Route(makeRequest("x")); !errors.Is(err, ErrNoHealthyDistributor) {
		t.Errorf("got %v, want ErrNoHealthyDistributor", err)
	}
}

func TestPool_ErrAllFull(t *testing.T) {
	p := NewPool(2, 10*time.Second, 100, 1, noopFlush)

	// Fill both queues (maxQueueDepth=1).
	if err := p.Route(makeRequest("a")); err != nil {
		t.Fatalf("first Route: %v", err)
	}
	if err := p.Route(makeRequest("b")); err != nil {
		t.Fatalf("second Route: %v", err)
	}

	if err := p.Route(makeRequest("c")); !errors.Is(err, ErrAllFull) {
		t.Errorf("got %v, want ErrAllFull", err)
	}
}

func TestPool_MarkHealthy(t *testing.T) {
	p := NewPool(1, 10*time.Second, 100, 500, noopFlush)
	p.MarkRecovering(1)

	if err := p.Route(makeRequest("x")); !errors.Is(err, ErrNoHealthyDistributor) {
		t.Errorf("while recovering: got %v, want ErrNoHealthyDistributor", err)
	}

	p.MarkHealthy(1)

	if err := p.Route(makeRequest("y")); err != nil {
		t.Errorf("after MarkHealthy: %v", err)
	}
}

func TestPool_All(t *testing.T) {
	p := NewPool(3, 10*time.Second, 100, 500, noopFlush)
	all := p.All()
	if len(all) != 3 {
		t.Errorf("All() len = %d, want 3", len(all))
	}
}

// TestPool_ConcurrentRouteMux verifies that Route never returns ErrNoHealthyDistributor
// while at least one distributor is guaranteed healthy, under concurrent MarkRecovering
// and MarkHealthy mutations. Run with -race to also catch data races on Pool.mu.
func TestPool_ConcurrentRouteMux(t *testing.T) {
	// 3 distributors: goroutines flip indices 1 and 2; index 3 stays healthy always.
	p := NewPool(3, 10*time.Second, 100, 500, noopFlush)

	const goroutines = 20
	const iters = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			target := uint32(id%2 + 1) // flips only indices 1 and 2
			for range iters {
				if err := p.Route(makeRequest("addr")); errors.Is(err, ErrNoHealthyDistributor) {
					t.Errorf("Route returned ErrNoHealthyDistributor with distributor 3 always healthy")
				}
				if id%2 == 0 {
					p.MarkRecovering(target)
				} else {
					p.MarkHealthy(target)
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestPool_Healthy(t *testing.T) {
	p := NewPool(3, 10*time.Second, 100, 500, noopFlush)
	p.MarkRecovering(2)

	healthy := p.Healthy()
	if len(healthy) != 2 {
		t.Errorf("Healthy() len = %d, want 2", len(healthy))
	}
	for _, d := range healthy {
		if d.KeyIndex == 2 {
			t.Errorf("recovering distributor 2 appeared in Healthy()")
		}
	}
}
