package chain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// ----- stub broadcaster -----

type stubBroadcaster struct {
	sendResult  *tx.BroadcastResult
	sendErr     error
	multiResult *tx.BroadcastResult
	multiErr    error
	// counts for inspection
	sendCalls  int
	multiCalls int
}

func (s *stubBroadcaster) BuildAndBroadcast(_ context.Context, _ tx.SendRequest) (*tx.BroadcastResult, error) {
	s.sendCalls++
	return s.sendResult, s.sendErr
}

func (s *stubBroadcaster) BuildAndBroadcastMulti(_ context.Context, _ tx.BatchSendRequest) (*tx.BroadcastResult, error) {
	s.multiCalls++
	return s.multiResult, s.multiErr
}

func (*stubBroadcaster) Close() error { return nil }

// ----- helpers -----

func newTestChain(stub *stubBroadcaster) *Chain {
	return &Chain{
		info:   &chainregistry.ChainInfo{ChainID: "test-1"},
		client: stub,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// makeReqs creates n batch requests each with a buffered result channel.
func makeReqs(n int) (reqs []batch.Request, chs []chan batch.Result) {
	reqs = make([]batch.Request, n)
	chs = make([]chan batch.Result, n)
	for i := range reqs {
		ch := make(chan batch.Result, 1)
		chs[i] = ch
		reqs[i] = batch.Request{
			ToAddress: "cosmos1test",
			Coins:     tx.Coins{{Denom: "uatom", Amount: "1000000"}},
			Result:    ch,
		}
	}
	return reqs, chs
}

func collectResults(t *testing.T, chs []chan batch.Result) []batch.Result {
	t.Helper()
	out := make([]batch.Result, len(chs))
	for i, ch := range chs {
		select {
		case r := <-ch:
			out[i] = r
		case <-time.After(2 * time.Second):
			t.Fatalf("result %d: timeout", i)
		}
	}
	return out
}

// ----- tests -----

func TestFlush_multiSendSuccess(t *testing.T) {
	stub := &stubBroadcaster{
		multiResult: &tx.BroadcastResult{TxHash: "MULTI1"},
	}
	c := newTestChain(stub)
	reqs, chs := makeReqs(3)

	c.makeFlushFn()(1, reqs)

	results := collectResults(t, chs)
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("req %d: unexpected error: %v", i, r.Err)
		}
		if r.TxHash != "MULTI1" {
			t.Errorf("req %d: TxHash = %q, want MULTI1", i, r.TxHash)
		}
	}
	if stub.multiCalls != 1 {
		t.Errorf("multiCalls = %d, want 1", stub.multiCalls)
	}
	if stub.sendCalls != 0 {
		t.Errorf("sendCalls = %d, want 0", stub.sendCalls)
	}
}

func TestFlush_multiSendRetryOnSequenceMismatch(t *testing.T) {
	callCount := 0
	stub := &stubBroadcaster{}
	c := newTestChain(stub)

	// First call: sequence mismatch; second call: success.
	c.client = &countingBroadcaster{
		multiResults: []*tx.BroadcastResult{nil, {TxHash: "RETRY1"}},
		multiErrors:  []error{tx.ErrSequenceMismatch, nil},
		callCount:    &callCount,
	}

	reqs, chs := makeReqs(2)
	c.makeFlushFn()(1, reqs)

	results := collectResults(t, chs)
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("unexpected error: %v", r.Err)
		}
		if r.TxHash != "RETRY1" {
			t.Errorf("TxHash = %q, want RETRY1", r.TxHash)
		}
	}
	if callCount != 2 {
		t.Errorf("multiSend calls = %d, want 2 (initial + retry)", callCount)
	}
}

func TestFlush_multiSendFails_splitSucceeds(t *testing.T) {
	stub := &stubBroadcaster{
		multiErr:   errors.New("multi failed"),
		sendResult: &tx.BroadcastResult{TxHash: "SPLIT1"},
	}
	c := newTestChain(stub)
	reqs, chs := makeReqs(2)

	c.makeFlushFn()(1, reqs)

	results := collectResults(t, chs)
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("unexpected error: %v", r.Err)
		}
		if r.TxHash != "SPLIT1" {
			t.Errorf("TxHash = %q, want SPLIT1", r.TxHash)
		}
	}
	if c.multiSendFailStreak.Load() != 1 {
		t.Errorf("multiSendFailStreak = %d, want 1", c.multiSendFailStreak.Load())
	}
	if c.sendFailStreak.Load() != 0 {
		t.Errorf("sendFailStreak = %d, want 0 after split success", c.sendFailStreak.Load())
	}
}

func TestFlush_multiSendDisabledAtThreshold(t *testing.T) {
	stub := &stubBroadcaster{
		multiErr:   errors.New("multi always fails"),
		sendResult: &tx.BroadcastResult{TxHash: "SPLIT"},
	}
	c := newTestChain(stub)

	// Need a pool for MarkRecovering calls — use a dummy pool (nil = no pool, so
	// sendFail branch skips MarkRecovering). Just test multi streak here.
	for range multiSendDisableThreshold {
		reqs, chs := makeReqs(1)
		c.makeFlushFn()(1, reqs)
		collectResults(t, chs)
	}

	if !c.multiSendDisabled.Load() {
		t.Error("multiSendDisabled should be true after 3 multi-send failures")
	}
}

func TestFlush_suspended(t *testing.T) {
	stub := &stubBroadcaster{}
	c := newTestChain(stub)
	c.suspended.Store(true)

	reqs, chs := makeReqs(2)
	c.makeFlushFn()(1, reqs)

	results := collectResults(t, chs)
	for _, r := range results {
		if !errors.Is(r.Err, ErrChainSuspended) {
			t.Errorf("expected ErrChainSuspended, got %v", r.Err)
		}
	}
	if stub.multiCalls != 0 || stub.sendCalls != 0 {
		t.Error("suspended chain should not call broadcaster")
	}
}

func TestChain_Pour_suspended(t *testing.T) {
	c := newTestChain(&stubBroadcaster{})
	c.suspended.Store(true)

	ch := make(chan batch.Result, 1)
	err := c.Pour(context.Background(), batch.Request{Result: ch})
	if !errors.Is(err, ErrChainSuspended) {
		t.Errorf("expected ErrChainSuspended, got %v", err)
	}
}

func TestChain_Pour_syncMode(t *testing.T) {
	c := newTestChain(&stubBroadcaster{}) // pool is nil → sync mode

	ch := make(chan batch.Result, 1)
	err := c.Pour(context.Background(), batch.Request{Result: ch})
	if !errors.Is(err, ErrSyncMode) {
		t.Errorf("expected ErrSyncMode, got %v", err)
	}
}

func TestChain_Resume(t *testing.T) {
	c := newTestChain(&stubBroadcaster{})
	c.suspended.Store(true)
	c.sendFailStreak.Store(3)
	c.multiSendFailStreak.Store(2)

	c.Resume()

	if c.suspended.Load() {
		t.Error("suspended should be false after Resume")
	}
	if c.sendFailStreak.Load() != 0 {
		t.Errorf("sendFailStreak = %d, want 0", c.sendFailStreak.Load())
	}
	if c.multiSendFailStreak.Load() != 0 {
		t.Errorf("multiSendFailStreak = %d, want 0", c.multiSendFailStreak.Load())
	}
}

func TestFlush_splitFails_streakIncrements(t *testing.T) {
	stub := &stubBroadcaster{
		multiErr: errors.New("multi failed"),
		sendErr:  errors.New("send failed"),
	}
	c := newTestChain(stub)

	reqs, chs := makeReqs(2)
	c.makeFlushFn()(1, reqs)
	collectResults(t, chs)

	if c.sendFailStreak.Load() != 1 {
		t.Errorf("sendFailStreak = %d, want 1", c.sendFailStreak.Load())
	}
}

func TestFlush_splitFails_suspendAtThreshold(t *testing.T) {
	stub := &stubBroadcaster{
		multiErr: errors.New("multi failed"),
		sendErr:  errors.New("send failed"),
	}
	c := newTestChain(stub)

	for range suspendThreshold {
		reqs, chs := makeReqs(1)
		c.makeFlushFn()(1, reqs)
		collectResults(t, chs)
	}

	if !c.suspended.Load() {
		t.Error("chain should be suspended after 5 consecutive send failures")
	}
}

func TestFlush_multiSendDisabled_skipsToSplit(t *testing.T) {
	stub := &stubBroadcaster{
		sendResult: &tx.BroadcastResult{TxHash: "SPLIT2"},
	}
	c := newTestChain(stub)
	c.multiSendDisabled.Store(true)

	reqs, chs := makeReqs(2)
	c.makeFlushFn()(1, reqs)

	results := collectResults(t, chs)
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("unexpected error: %v", r.Err)
		}
		if r.TxHash != "SPLIT2" {
			t.Errorf("TxHash = %q, want SPLIT2", r.TxHash)
		}
	}
	if stub.multiCalls != 0 {
		t.Errorf("multiCalls = %d, want 0 (multi-send disabled)", stub.multiCalls)
	}
	// multiSendFailStreak must not change when multi was not attempted.
	if c.multiSendFailStreak.Load() != 0 {
		t.Errorf("multiSendFailStreak = %d, want 0", c.multiSendFailStreak.Load())
	}
}

func TestFlush_successResetsStreaks(t *testing.T) {
	stub := &stubBroadcaster{
		multiResult: &tx.BroadcastResult{TxHash: "OK"},
	}
	c := newTestChain(stub)
	c.sendFailStreak.Store(3)
	c.multiSendFailStreak.Store(2)

	reqs, chs := makeReqs(1)
	c.makeFlushFn()(1, reqs)
	collectResults(t, chs)

	if c.sendFailStreak.Load() != 0 {
		t.Errorf("sendFailStreak = %d, want 0 after success", c.sendFailStreak.Load())
	}
	if c.multiSendFailStreak.Load() != 0 {
		t.Errorf("multiSendFailStreak = %d, want 0 after success", c.multiSendFailStreak.Load())
	}
}

// ----- countingBroadcaster for retry tests -----

type countingBroadcaster struct {
	multiResults []*tx.BroadcastResult
	multiErrors  []error
	callCount    *int
}

func (*countingBroadcaster) BuildAndBroadcast(_ context.Context, _ tx.SendRequest) (*tx.BroadcastResult, error) {
	return nil, errors.New("not expected")
}

func (b *countingBroadcaster) BuildAndBroadcastMulti(_ context.Context, _ tx.BatchSendRequest) (*tx.BroadcastResult, error) {
	i := *b.callCount
	*b.callCount++
	if i < len(b.multiResults) {
		return b.multiResults[i], b.multiErrors[i]
	}
	return nil, errors.New("unexpected extra call")
}

func (*countingBroadcaster) Close() error { return nil }
