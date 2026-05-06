package batch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

func makeRequest(addr string) Request {
	return Request{
		ToAddress: addr,
		Coins:     tx.Coins{{Denom: "uatom", Amount: "1000000"}},
	}
}

func TestWindow_TimerFire(t *testing.T) {
	flushed := make(chan []Request, 1)
	w := NewWindow(50*time.Millisecond, 100, 500, func(batch []Request) {
		flushed <- batch
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	if err := w.Enqueue(makeRequest("addr1")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case batch := <-flushed:
		if len(batch) != 1 || batch[0].ToAddress != "addr1" {
			t.Errorf("unexpected batch: %v", batch)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer did not fire within 200ms")
	}
}

func TestWindow_CapFire(t *testing.T) {
	flushed := make(chan []Request, 1)
	w := NewWindow(10*time.Second, 3, 500, func(batch []Request) {
		flushed <- batch
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	for range 3 {
		if err := w.Enqueue(makeRequest("addr")); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	select {
	case batch := <-flushed:
		if len(batch) != 3 {
			t.Errorf("got batch len %d, want 3", len(batch))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cap-triggered flush did not fire within 500ms")
	}
}

func TestWindow_ErrQueueFull(t *testing.T) {
	w := NewWindow(10*time.Second, 100, 2, func([]Request) {})

	if err := w.Enqueue(makeRequest("a")); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := w.Enqueue(makeRequest("b")); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if err := w.Enqueue(makeRequest("c")); !errors.Is(err, ErrQueueFull) {
		t.Errorf("third enqueue: got %v, want ErrQueueFull", err)
	}
}

func TestWindow_ResultDelivery(t *testing.T) {
	resultCh := make(chan Result, 1)
	w := NewWindow(50*time.Millisecond, 100, 500, func(batch []Request) {
		for _, req := range batch {
			if req.Result != nil {
				req.Result <- Result{TxHash: "abc123"}
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	req := makeRequest("addr1")
	req.Result = resultCh
	if err := w.Enqueue(req); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case res := <-resultCh:
		if res.TxHash != "abc123" {
			t.Errorf("got TxHash %q, want %q", res.TxHash, "abc123")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("result not delivered within 200ms")
	}
}

func TestWindow_CapLimitsFlushSize(t *testing.T) {
	flushSizes := make(chan int, 10)
	// maxRecipients=2 so each flush takes at most 2 items.
	w := NewWindow(10*time.Second, 2, 10, func(batch []Request) {
		flushSizes <- len(batch)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enqueue 5 items before starting so all are buffered.
	for range 5 {
		if err := w.Enqueue(makeRequest("addr")); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	w.Start(ctx)

	// The cap signal fires when queue ≥ maxRecipients; first flush must be exactly 2.
	select {
	case n := <-flushSizes:
		if n != 2 {
			t.Errorf("first flush size = %d, want 2", n)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first flush did not fire within 500ms")
	}

	// 3 items must remain — they overflow to the next window tick.
	if d := w.Depth(); d != 3 {
		t.Errorf("queue depth after first flush = %d, want 3", d)
	}
}

func TestWindow_Depth(t *testing.T) {
	w := NewWindow(10*time.Second, 100, 500, func([]Request) {})

	if d := w.Depth(); d != 0 {
		t.Errorf("initial depth %d, want 0", d)
	}
	_ = w.Enqueue(makeRequest("a"))
	_ = w.Enqueue(makeRequest("b"))
	if d := w.Depth(); d != 2 {
		t.Errorf("depth after 2 enqueues: %d, want 2", d)
	}
}
