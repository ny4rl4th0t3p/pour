package chain

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/observability"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// txBroadcaster abstracts tx.Client for chain-level testability.
type txBroadcaster interface {
	BuildAndBroadcast(ctx context.Context, req tx.SendRequest) (*tx.BroadcastResult, error)
	BuildAndBroadcastMulti(ctx context.Context, req tx.BatchSendRequest) (*tx.BroadcastResult, error)
	BuildAndBroadcastTransfer(ctx context.Context, req tx.TransferRequest) (tx.TransferResult, error)
	QueryBalance(ctx context.Context, address, denom string) (tx.Coin, error)
	Close() error
}

// Chain is an active, connected chain managed by Manager.
type Chain struct {
	info             *chainregistry.ChainInfo
	drip             chainregistry.DripPolicy
	client           txBroadcaster
	endpointPool     *tx.EndpointPool
	pool             *batch.Pool // nil in sync mode (BatchWindow == 0)
	holderAddr       string
	distributorAddrs []string // key indices 1..N
	refillThreshold  tx.Coin
	refillInterval   time.Duration
	ibcTimeout       time.Duration
	ibcDrips         []config.IBCDripConfig
	log              *slog.Logger

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
	ibcTimeout, _ := time.ParseDuration(cfg.IBC.Timeout) // already validated

	hasEndpoints := len(info.Endpoints.GRPC) > 0 || len(info.Endpoints.REST) > 0

	// IBC-only destination: no endpoints means no tx client is possible.
	// All drips arrive via MsgTransfer from the source chain's wallet.
	if drip.Anonymous == "" && !hasEndpoints {
		log.Debug("chain is IBC-only destination, no tx client created", "chain_id", info.ChainID)
		return &Chain{
			info:       info,
			drip:       drip,
			ibcTimeout: ibcTimeout,
			ibcDrips:   cfg.IBC.Drips,
			log:        log,
		}, nil
	}

	var epPool *tx.EndpointPool
	if len(info.Endpoints.GRPC) > 0 {
		grpcURLs := make([]string, len(info.Endpoints.GRPC))
		for i, ep := range info.Endpoints.GRPC {
			grpcURLs[i] = ep.URL
		}
		epPool = tx.NewEndpointPool(grpcURLs)
	}

	// Source-only chains (endpoints but no native drip) only need the holder key (index 0)
	// for MsgTransfer. Native drip chains also derive distributor keys 1..N.
	n := 0
	if drip.Anonymous != "" {
		n = cfg.DistributorCount()
	}
	keyIndices := make([]uint32, n+1)
	for i := range keyIndices {
		keyIndices[i] = uint32(i) // 0=holder, 1..N=distributors
	}

	client, err := tx.New(info, mnemonic, keyIndices, tx.Options{GasCache: gc, EndpointPool: epPool})
	if err != nil {
		return nil, err
	}

	// Source-only chain: has tx client for MsgTransfer but no native drip infrastructure.
	if drip.Anonymous == "" {
		log.Debug("chain is IBC source-only, no native drip", "chain_id", info.ChainID)
		return &Chain{
			info:         info,
			drip:         drip,
			client:       client,
			endpointPool: epPool,
			ibcTimeout:   ibcTimeout,
			ibcDrips:     cfg.IBC.Drips,
			log:          log,
		}, nil
	}

	batchDur, err := cfg.BatchWindowDuration()
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	holderAddr, err := client.AddressForKey(0)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("chain %q: derive holder address: %w", info.ChainID, err)
	}
	distributorAddrs := make([]string, n)
	for i := range n {
		distributorAddrs[i], err = client.AddressForKey(uint32(i + 1))
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("chain %q: derive distributor %d address: %w", info.ChainID, i+1, err)
		}
	}

	refillThreshold, err := resolveRefillThreshold(cfg.RefillThreshold, drip.Anonymous)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("chain %q: refill threshold: %w", info.ChainID, err)
	}
	refillInterval, err := cfg.RefillIntervalOrDefault()
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	c := &Chain{
		info:             info,
		drip:             drip,
		client:           client,
		endpointPool:     epPool,
		holderAddr:       holderAddr,
		distributorAddrs: distributorAddrs,
		refillThreshold:  refillThreshold,
		refillInterval:   refillInterval,
		ibcTimeout:       ibcTimeout,
		ibcDrips:         cfg.IBC.Drips,
		log:              log,
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

// resolveRefillThreshold returns the configured threshold, or computes the default
// (dripAnonymous × 10) when threshold is empty.
func resolveRefillThreshold(threshold, dripAnonymous string) (tx.Coin, error) {
	if threshold != "" {
		return config.ParseCoin(threshold)
	}
	if dripAnonymous == "" {
		return tx.Coin{}, fmt.Errorf("refill_threshold not set and drip.anonymous is empty")
	}
	coin, err := config.ParseCoin(dripAnonymous)
	if err != nil {
		return tx.Coin{}, err
	}
	amount := new(big.Int)
	if _, ok := amount.SetString(coin.Amount, 10); !ok {
		return tx.Coin{}, fmt.Errorf("invalid drip amount %q", coin.Amount)
	}
	amount.Mul(amount, big.NewInt(10))
	return tx.Coin{Denom: coin.Denom, Amount: amount.String()}, nil
}

// DistributorState holds the observable state of one distributor account.
type DistributorState struct {
	KeyIndex   uint32
	Address    string
	Balance    string // coin amount; empty if balance query failed
	QueueDepth int
	Status     batch.Status
}

// ChainStatusSnapshot is a point-in-time view of chain operational flags.
type ChainStatusSnapshot struct {
	Suspended           bool
	SuspendReason       string
	MultiSendDisabled   bool
	SendFailStreak      int32
	MultiSendFailStreak int32
}

// Info returns the chain's resolved configuration.
func (c *Chain) Info() *chainregistry.ChainInfo { return c.info }

// Drip returns the drip policy for this chain.
func (c *Chain) Drip() chainregistry.DripPolicy { return c.drip }

// IBCTimeout returns the configured MsgTransfer timeout duration for this chain.
func (c *Chain) IBCTimeout() time.Duration { return c.ibcTimeout }

// IBCDrips returns the list of IBC drip configurations for this chain.
// Each entry defines a token transferred from a source chain via MsgTransfer.
func (c *Chain) IBCDrips() []config.IBCDripConfig { return c.ibcDrips }

// Client returns the underlying *tx.Client. Returns nil for test chains backed by stubs.
func (c *Chain) Client() *tx.Client {
	if cl, ok := c.client.(*tx.Client); ok {
		return cl
	}
	return nil
}

// Close closes the underlying gRPC connection.
func (c *Chain) Close() {
	if c.client != nil {
		_ = c.client.Close()
	}
}

// Pour routes req to the distributor pool. Returns ErrChainSuspended or ErrSyncMode
// when the chain cannot accept the request.
func (c *Chain) Pour(_ context.Context, req batch.Request) error {
	if c.suspended.Load() {
		return ErrChainSuspended
	}
	if c.pool == nil {
		return ErrSyncMode
	}
	if err := c.pool.Route(req); err != nil {
		return err
	}
	for _, d := range c.pool.All() {
		observability.QueueDepth.WithLabelValues(c.info.ChainID, fmt.Sprintf("%d", d.KeyIndex)).Set(float64(d.Window.Depth()))
	}
	return nil
}

// Start launches the pool goroutines (if batching is enabled), the endpoint probe loop,
// and the holder refill loop.
func (c *Chain) Start(ctx context.Context) {
	if c.pool != nil {
		c.pool.Start(ctx)
	}
	if c.endpointPool != nil {
		startProbeLoop(ctx, c.endpointPool, c.log)
	}
	if c.client != nil {
		go c.RefillLoop(ctx)
	}
}

// Suspend marks the chain as suspended and logs the reason.
func (c *Chain) Suspend(err error) {
	reason := err.Error()
	c.suspendReason.Store(&reason)
	c.suspended.Store(true)
	observability.ChainSuspended.WithLabelValues(c.info.ChainID).Set(1)
	c.log.Error("chain: suspended — resume with 'pour admin resume CHAIN_ID'",
		"chain_id", c.info.ChainID, "err", err)
}

// Resume clears the suspended state and resets failure streaks.
func (c *Chain) Resume() {
	c.suspended.Store(false)
	c.sendFailStreak.Store(0)
	c.multiSendFailStreak.Store(0)
	observability.ChainSuspended.WithLabelValues(c.info.ChainID).Set(0)
	c.log.Info("chain: resumed", "chain_id", c.info.ChainID)
}

// DistributorStates returns a best-effort snapshot of all distributor accounts.
// Balance is queried live from the node; an empty string means the query failed.
func (c *Chain) DistributorStates(ctx context.Context) []DistributorState {
	var poolDists []*batch.Distributor
	if c.pool != nil {
		poolDists = c.pool.All()
	}
	states := make([]DistributorState, len(c.distributorAddrs))
	for i, addr := range c.distributorAddrs {
		bal, err := c.client.QueryBalance(ctx, addr, c.refillThreshold.Denom)
		balStr := ""
		if err == nil {
			balStr = bal.Amount
		}
		depth := 0
		status := batch.StatusHealthy
		if i < len(poolDists) {
			depth = poolDists[i].Window.Depth()
			status = poolDists[i].Status()
		}
		states[i] = DistributorState{
			KeyIndex:   uint32(i + 1),
			Address:    addr,
			Balance:    balStr,
			QueueDepth: depth,
			Status:     status,
		}
	}
	return states
}

// ChainStatus returns a point-in-time view of chain operational flags.
func (c *Chain) ChainStatus() ChainStatusSnapshot {
	snap := ChainStatusSnapshot{
		Suspended:           c.suspended.Load(),
		MultiSendDisabled:   c.multiSendDisabled.Load(),
		SendFailStreak:      c.sendFailStreak.Load(),
		MultiSendFailStreak: c.multiSendFailStreak.Load(),
	}
	if r := c.suspendReason.Load(); r != nil {
		snap.SuspendReason = *r
	}
	return snap
}

// makeFlushFn returns the batch flush callback for this chain.
func (c *Chain) makeFlushFn() func(uint32, []batch.Request) {
	return func(keyIndex uint32, reqs []batch.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()

		if c.suspended.Load() {
			observability.DripsTotal.WithLabelValues(c.info.ChainID, "failed").Add(float64(len(reqs)))
			sendErrAll(reqs, ErrChainSuspended)
			return
		}

		observability.BatchSizeRecipients.WithLabelValues(c.info.ChainID).Observe(float64(len(reqs)))

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
				observability.DripsTotal.WithLabelValues(c.info.ChainID, "confirmed").Add(float64(len(reqs)))
				for _, r := range reqs {
					r.Result <- batch.Result{TxHash: result.TxHash}
				}
				return
			}
			// Fall through to per-output split.
		}

		// Split: send one MsgSend per output, round-robining across healthy distributors
		// to avoid sequence contention on a single account (§2.11.3).
		var healthy []*batch.Distributor
		if c.pool != nil {
			healthy = c.pool.Healthy()
		}
		for i, r := range reqs {
			ki := keyIndex
			if len(healthy) > 0 {
				ki = healthy[i%len(healthy)].KeyIndex
			}
			result, err := c.client.BuildAndBroadcast(ctx, tx.SendRequest{
				KeyIndex:  ki,
				ToAddress: r.ToAddress,
				Coins:     r.Coins,
			})
			if err != nil {
				if streak := c.sendFailStreak.Add(1); streak >= suspendThreshold {
					c.Suspend(fmt.Errorf("send streak %d: %w", streak, err))
				}
				if c.pool != nil {
					c.pool.MarkRecovering(ki)
					observability.DistributorRecoveryTotal.WithLabelValues(c.info.ChainID).Inc()
				}
				observability.DripsTotal.WithLabelValues(c.info.ChainID, "failed").Add(float64(len(reqs[i:])))
				sendErrAll(reqs[i:], err)
				return
			}
			observability.DripsTotal.WithLabelValues(c.info.ChainID, "confirmed").Inc()
			r.Result <- batch.Result{TxHash: result.TxHash}
		}
		// Split succeeded: if multi was attempted and failed, increment its streak.
		if multiAttempted {
			if streak := c.multiSendFailStreak.Add(1); streak >= multiSendDisableThreshold {
				if !c.multiSendDisabled.Swap(true) {
					c.log.Warn("chain: MsgMultiSend disabled after repeated failures",
						"chain_id", c.info.ChainID)
					observability.MultisendDisabledTotal.WithLabelValues(c.info.ChainID).Inc()
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
