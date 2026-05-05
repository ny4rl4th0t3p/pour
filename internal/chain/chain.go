package chain

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// txBroadcaster abstracts tx.Client for chain-level testability.
type txBroadcaster interface {
	BuildAndBroadcast(ctx context.Context, req tx.SendRequest) (*tx.BroadcastResult, error)
	BuildAndBroadcastMulti(ctx context.Context, req tx.BatchSendRequest) (*tx.BroadcastResult, error)
	Close() error
}

// Chain is an active, connected chain managed by Manager.
type Chain struct {
	info         *chainregistry.ChainInfo
	drip         chainregistry.DripPolicy
	client       txBroadcaster
	endpointPool *tx.EndpointPool
	pool         *batch.Pool // nil in sync mode (BatchWindow == 0)
	log          *slog.Logger

	multiSendDisabled   atomic.Bool
	suspended           atomic.Bool
	suspendReason       atomic.Pointer[string]
	sendFailStreak      atomic.Int32
	multiSendFailStreak atomic.Int32
}

const (
	multiSendDisableThreshold = 3
	suspendThreshold          = 5
	flushTimeout              = 30 * time.Second
)

func newChain(
	info *chainregistry.ChainInfo,
	drip chainregistry.DripPolicy,
	gc tx.GasCache,
	mnemonic string,
	cfg config.ChainConfig,
	log *slog.Logger,
) (*Chain, error) {
	n := cfg.DistributorCount()
	keyIndices := make([]uint32, n+1)
	for i := range keyIndices {
		keyIndices[i] = uint32(i) // 0=holder, 1..N=distributors
	}

	grpcURLs := make([]string, len(info.Endpoints.GRPC))
	for i, ep := range info.Endpoints.GRPC {
		grpcURLs[i] = ep.URL
	}
	epPool := tx.NewEndpointPool(grpcURLs)

	client, err := tx.New(info, mnemonic, keyIndices, tx.Options{GasCache: gc, EndpointPool: epPool})
	if err != nil {
		return nil, err
	}

	batchDur, err := cfg.BatchWindowDuration()
	if err != nil {
		return nil, err
	}

	c := &Chain{
		info:         info,
		drip:         drip,
		client:       client,
		endpointPool: epPool,
		log:          log,
	}

	if batchDur > 0 {
		c.pool = batch.NewPool(
			n,
			batchDur,
			cfg.MaxRecipientsPerBatchOrDefault(),
			cfg.MaxQueueDepthOrDefault(),
			c.makeFlushFn(),
		)
	}

	return c, nil
}

// Info returns the chain's resolved configuration.
func (c *Chain) Info() *chainregistry.ChainInfo { return c.info }

// Drip returns the drip policy for this chain.
func (c *Chain) Drip() chainregistry.DripPolicy { return c.drip }

// Client returns the underlying *tx.Client. Returns nil for test chains backed by stubs.
func (c *Chain) Client() *tx.Client {
	if cl, ok := c.client.(*tx.Client); ok {
		return cl
	}
	return nil
}

// Close closes the underlying gRPC connection.
func (c *Chain) Close() { _ = c.client.Close() }

// Pour routes req to the distributor pool. Returns ErrChainSuspended or ErrSyncMode
// when the chain cannot accept the request.
func (c *Chain) Pour(_ context.Context, req batch.Request) error {
	if c.suspended.Load() {
		return ErrChainSuspended
	}
	if c.pool == nil {
		return ErrSyncMode
	}
	return c.pool.Route(req)
}

// Start launches the pool goroutines (if batching is enabled) and the endpoint probe loop.
func (c *Chain) Start(ctx context.Context) {
	if c.pool != nil {
		c.pool.Start(ctx)
	}
	if c.endpointPool != nil {
		startProbeLoop(ctx, c.endpointPool, c.log)
	}
}

// Suspend marks the chain as suspended and logs the reason.
func (c *Chain) Suspend(err error) {
	reason := err.Error()
	c.suspendReason.Store(&reason)
	c.suspended.Store(true)
	c.log.Error("chain: suspended — resume with 'pour admin resume CHAIN_ID'",
		"chain_id", c.info.ChainID, "err", err)
}

// Resume clears the suspended state and resets failure streaks.
func (c *Chain) Resume() {
	c.suspended.Store(false)
	c.sendFailStreak.Store(0)
	c.multiSendFailStreak.Store(0)
	c.log.Info("chain: resumed", "chain_id", c.info.ChainID)
}

// makeFlushFn returns the batch flush callback for this chain.
func (c *Chain) makeFlushFn() func(uint32, []batch.Request) {
	return func(keyIndex uint32, reqs []batch.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()

		if c.suspended.Load() {
			sendErrAll(reqs, ErrChainSuspended)
			return
		}

		outputs := make([]tx.SendOutput, len(reqs))
		for i, r := range reqs {
			outputs[i] = tx.SendOutput{ToAddress: r.ToAddress, Coins: r.Coins}
		}

		// Attempt MsgMultiSend unless disabled by persistent failures.
		multiAttempted := false
		if !c.multiSendDisabled.Load() {
			multiAttempted = true
			result, err := c.client.BuildAndBroadcastMulti(ctx, tx.BatchSendRequest{
				KeyIndex: keyIndex,
				Outputs:  outputs,
			})
			if err != nil && (tx.IsSequenceMismatch(err) || tx.IsInsufficientFee(err)) {
				result, err = c.client.BuildAndBroadcastMulti(ctx, tx.BatchSendRequest{
					KeyIndex: keyIndex,
					Outputs:  outputs,
				})
			}
			if err == nil {
				c.sendFailStreak.Store(0)
				c.multiSendFailStreak.Store(0)
				for _, r := range reqs {
					r.Result <- batch.Result{TxHash: result.TxHash}
				}
				return
			}
			// Fall through to per-output split.
		}

		// Split: send one MsgSend per output.
		for i, r := range reqs {
			result, err := c.client.BuildAndBroadcast(ctx, tx.SendRequest{
				KeyIndex:  keyIndex,
				ToAddress: r.ToAddress,
				Coins:     r.Coins,
			})
			if err != nil {
				if streak := c.sendFailStreak.Add(1); streak >= suspendThreshold {
					c.Suspend(fmt.Errorf("send streak %d: %w", streak, err))
				}
				if c.pool != nil {
					c.pool.MarkRecovering(keyIndex)
				}
				sendErrAll(reqs[i:], err)
				return
			}
			r.Result <- batch.Result{TxHash: result.TxHash}
		}
		// Split succeeded: if multi was attempted and failed, increment its streak.
		if multiAttempted {
			if streak := c.multiSendFailStreak.Add(1); streak >= multiSendDisableThreshold {
				if !c.multiSendDisabled.Swap(true) {
					c.log.Warn("chain: MsgMultiSend disabled after repeated failures",
						"chain_id", c.info.ChainID)
				}
			}
		}
		c.sendFailStreak.Store(0)
	}
}

// sendErrAll sends err to every request's Result channel.
func sendErrAll(reqs []batch.Request, err error) {
	for _, r := range reqs {
		r.Result <- batch.Result{Err: err}
	}
}
