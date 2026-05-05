package batch

import (
	"context"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// Request is a single pour request waiting to be batched.
type Request struct {
	ToAddress string
	Coins     tx.Coins
	Result    chan<- Result
}

// Result is the outcome of a batched request, delivered via Request.Result.
type Result struct {
	TxHash string
	Err    error
}

// Window collects requests over a time interval and flushes them as a batch.
// duration must be > 0; callers are responsible for not creating a Window when
// batching is disabled (batch_window = "0").
type Window struct {
	duration      time.Duration
	maxRecipients int
	queue         chan Request
	flushNow      chan struct{}
	flush         func([]Request)
}

// NewWindow creates a Window. flush is called with a non-empty batch on each fire.
func NewWindow(duration time.Duration, maxRecipients, maxQueueDepth int, flush func([]Request)) *Window {
	return &Window{
		duration:      duration,
		maxRecipients: maxRecipients,
		queue:         make(chan Request, maxQueueDepth),
		flushNow:      make(chan struct{}, 1),
		flush:         flush,
	}
}

// Enqueue adds req to the window. Returns ErrQueueFull when at capacity.
// If the queue reaches maxRecipients after this enqueue, an early flush is triggered.
func (w *Window) Enqueue(req Request) error {
	select {
	case w.queue <- req:
	default:
		return ErrQueueFull
	}
	if len(w.queue) >= w.maxRecipients {
		select {
		case w.flushNow <- struct{}{}:
		default: // already signaled
		}
	}
	return nil
}

// Start launches the timer goroutine. Stops when ctx is canceled, flushing any
// remaining requests before returning.
func (w *Window) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(w.duration)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				w.doFlush()
				return
			case <-ticker.C:
				w.doFlush()
			case <-w.flushNow:
				w.doFlush()
				ticker.Reset(w.duration)
			}
		}
	}()
}

// Depth returns the current number of queued requests.
func (w *Window) Depth() int { return len(w.queue) }

func (w *Window) doFlush() {
	batch := make([]Request, 0, len(w.queue))
loop:
	for {
		select {
		case req := <-w.queue:
			batch = append(batch, req)
		default:
			break loop
		}
	}
	if len(batch) > 0 {
		w.flush(batch)
	}
}
