package tx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// Options are optional parameters for New.
type Options struct {
	GasCache     GasCache      // optional; read + write gas cache
	Logger       *slog.Logger  // optional; defaults to slog.Default()
	EndpointPool *EndpointPool // optional; enables gRPC endpoint failover
}

// Client is a single-chain tx client. All private keys are pre-derived at construction;
// the mnemonic is not retained after New returns.
type Client struct {
	chain      *chainregistry.ChainInfo
	active     transport
	grpcPool   *EndpointPool
	restPool   *EndpointPool
	usingREST  bool
	cachedKeys map[uint32]*keys.PrivKey
	opts       Options
}

// New derives private keys for all keyIndices eagerly, then connects using the first
// available endpoint (gRPC preferred; REST if no gRPC). The mnemonic string is used
// only during New and is not stored in the returned Client.
func New(chain *chainregistry.ChainInfo, mnemonic string, keyIndices []uint32, opts Options) (*Client, error) {
	if len(chain.Endpoints.GRPC) == 0 && len(chain.Endpoints.REST) == 0 {
		return nil, fmt.Errorf("tx: chain %q: no gRPC or REST endpoints configured", chain.ChainID)
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	cachedKeys := make(map[uint32]*keys.PrivKey, len(keyIndices))
	for _, idx := range keyIndices {
		k, err := keys.DerivePrivKey(mnemonic, chain.Slip44, idx)
		if err != nil {
			return nil, fmt.Errorf("tx: derive key index %d: %w", idx, err)
		}
		cachedKeys[idx] = k
	}

	var grpcPool *EndpointPool
	if len(chain.Endpoints.GRPC) > 0 {
		grpcURLs := make([]string, len(chain.Endpoints.GRPC))
		for i, ep := range chain.Endpoints.GRPC {
			grpcURLs[i] = ep.URL
		}
		grpcPool = NewEndpointPool(grpcURLs)
	}
	// Prefer the caller-supplied pool over the one built from chain info.
	if opts.EndpointPool != nil {
		grpcPool = opts.EndpointPool
	}

	var restPool *EndpointPool
	if len(chain.Endpoints.REST) > 0 {
		restURLs := make([]string, len(chain.Endpoints.REST))
		for i, ep := range chain.Endpoints.REST {
			restURLs[i] = ep.URL
		}
		restPool = NewEndpointPool(restURLs)
	}

	var active transport
	usingREST := false

	if grpcPool != nil {
		initialURL, ok := grpcPool.Next()
		if !ok {
			return nil, ErrNoEndpointAvailable
		}
		b, err := newGRPCTransport(initialURL)
		if err != nil {
			return nil, fmt.Errorf("tx: dial %s: %w", initialURL, err)
		}
		active = b
	} else {
		if restPool == nil {
			return nil, ErrNoEndpointAvailable
		}
		initialURL, ok := restPool.Next()
		if !ok {
			return nil, ErrNoEndpointAvailable
		}
		active = newRESTTransport(initialURL)
		usingREST = true
	}

	return &Client{
		chain:      chain,
		active:     active,
		grpcPool:   grpcPool,
		restPool:   restPool,
		usingREST:  usingREST,
		cachedKeys: cachedKeys,
		opts:       opts,
	}, nil
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	return c.active.close()
}

// QueryBalance returns the balance of denom for address.
func (c *Client) QueryBalance(ctx context.Context, address, denom string) (Coin, error) {
	return c.active.queryBalance(ctx, address, denom)
}

// AddressForKey returns the bech32 address for the pre-derived key at keyIndex.
func (c *Client) AddressForKey(keyIndex uint32) (string, error) {
	k, ok := c.cachedKeys[keyIndex]
	if !ok {
		return "", fmt.Errorf("tx: key index %d not pre-derived", keyIndex)
	}
	return keys.AddressFromPubKey(k.PubKey(), c.chain.Bech32Prefix)
}

// BuildAndBroadcast looks up the pre-derived key at req.KeyIndex, builds a MsgSend,
// signs it, and broadcasts it. Records gas usage to Options.GasCache on success.
func (c *Client) BuildAndBroadcast(ctx context.Context, req SendRequest) (*BroadcastResult, error) {
	privKey, ok := c.cachedKeys[req.KeyIndex]
	if !ok {
		return nil, fmt.Errorf("tx: key index %d not pre-derived", req.KeyIndex)
	}
	fromAddr, err := keys.AddressFromPubKey(privKey.PubKey(), c.chain.Bech32Prefix)
	if err != nil {
		return nil, fmt.Errorf("tx: derive address: %w", err)
	}

	msgAny, err := buildMsgSend(fromAddr, req.ToAddress, req.Coins)
	if err != nil {
		return nil, err
	}

	return c.doSend(ctx, privKey, fromAddr, []*anypb.Any{msgAny}, MsgTypeSend, 1)
}

// BuildAndBroadcastMulti looks up the pre-derived key at req.KeyIndex, builds a
// MsgMultiSend, signs it, and broadcasts it. Records gas usage to Options.GasCache on success.
func (c *Client) BuildAndBroadcastMulti(ctx context.Context, req BatchSendRequest) (*BroadcastResult, error) {
	if len(req.Outputs) == 0 {
		return nil, fmt.Errorf("tx: BuildAndBroadcastMulti: outputs must not be empty")
	}

	privKey, ok := c.cachedKeys[req.KeyIndex]
	if !ok {
		return nil, fmt.Errorf("tx: key index %d not pre-derived", req.KeyIndex)
	}
	fromAddr, err := keys.AddressFromPubKey(privKey.PubKey(), c.chain.Bech32Prefix)
	if err != nil {
		return nil, fmt.Errorf("tx: derive address: %w", err)
	}

	msgAny, err := buildMsgMultiSend(fromAddr, req.Outputs)
	if err != nil {
		return nil, err
	}

	return c.doSend(ctx, privKey, fromAddr, []*anypb.Any{msgAny}, MsgTypeMultiSend, len(req.Outputs))
}

// maxSequenceRetries is the number of sequence-mismatch retries before giving up.
// Each retry waits sequenceRetryDelay for the competing tx to be committed.
// 3 retries × 2 s = 6 s total, covering networks with block times up to ~5 s.
const maxSequenceRetries = 3

// sequenceRetryDelay is the pause between sequence-mismatch retries. Exposed as a
// variable so unit tests can override it without sleeping.
var sequenceRetryDelay = 2 * time.Second

// doSend is the top-level send pipeline. It retries on ErrSequenceMismatch up to
// maxSequenceRetries times, waiting sequenceRetryDelay between each attempt so the
// competing tx has time to be committed before we re-query the account sequence.
// Endpoint failover (codes.Unavailable) is handled inside attemptSendWithFailover.
func (c *Client) doSend(
	ctx context.Context,
	privKey *keys.PrivKey,
	fromAddr string,
	msgs []*anypb.Any,
	msgType string,
	outputCount int,
) (*BroadcastResult, error) {
	for attempt := range maxSequenceRetries + 1 {
		result, err := c.attemptSendWithFailover(ctx, privKey, fromAddr, msgs, msgType, outputCount)
		if err == nil {
			return result, nil
		}
		if !IsSequenceMismatch(err) || attempt == maxSequenceRetries {
			return nil, err
		}
		if c.opts.Logger != nil {
			c.opts.Logger.WarnContext(ctx, "tx: sequence mismatch, retrying",
				"chain", c.chain.ChainID, "attempt", attempt+1)
		}
		if !sleepOrCancel(ctx, sequenceRetryDelay) {
			return nil, ctx.Err()
		}
	}
	panic("unreachable: doSend loop always returns on the final attempt")
}

// attemptSendWithFailover wraps attemptSend with one endpoint failover on
// codes.Unavailable. Sequence errors are not handled here — doSend owns those.
func (c *Client) attemptSendWithFailover(
	ctx context.Context,
	privKey *keys.PrivKey,
	fromAddr string,
	msgs []*anypb.Any,
	msgType string,
	outputCount int,
) (*BroadcastResult, error) {
	for range 2 {
		result, err := c.attemptSend(ctx, privKey, fromAddr, msgs, msgType, outputCount)
		if err == nil {
			return result, nil
		}
		if !isUnavailable(err) {
			return nil, err
		}

		// Mark the current endpoint unhealthy and try to get a next one.
		currentURL := c.active.endpointURL()
		activePool := c.grpcPool
		if c.usingREST {
			activePool = c.restPool
		}
		if activePool != nil {
			activePool.MarkUnhealthy(currentURL)
		}

		nextURL, ok := "", false
		if activePool != nil {
			nextURL, ok = activePool.Next()
		}

		if !ok && !c.usingREST && c.restPool != nil {
			// gRPC pool exhausted — fall over to REST.
			nextURL, ok = c.restPool.Next()
			if ok {
				_ = c.active.close()
				c.active = newRESTTransport(nextURL)
				c.usingREST = true
				continue
			}
		}

		if !ok {
			return nil, ErrNoEndpointAvailable
		}

		_ = c.active.close()
		if c.usingREST {
			c.active = newRESTTransport(nextURL)
		} else {
			b, dialErr := newGRPCTransport(nextURL)
			if dialErr != nil {
				return nil, fmt.Errorf("tx: failover dial %s: %w", nextURL, dialErr)
			}
			c.active = b
		}
	}
	return nil, ErrNoEndpointAvailable
}

func (c *Client) attemptSend(
	ctx context.Context,
	privKey *keys.PrivKey,
	fromAddr string,
	msgs []*anypb.Any,
	msgType string,
	outputCount int,
) (*BroadcastResult, error) {
	// Fee estimation (simulation) may advance the account sequence on some nodes.
	// Query the account after estimation so we always sign with the current sequence.
	estimate, err := estimateFee(ctx, c.active, c.chain, msgs,
		feeOpts{GasCache: c.opts.GasCache, OutputCount: outputCount, MsgType: msgType},
		c.opts.Logger)
	if err != nil {
		return nil, err
	}

	account, err := c.active.queryAccount(ctx, fromAddr)
	if err != nil {
		return nil, err
	}

	txRaw, err := buildTxRaw(privKey, *account, msgs, estimate, c.chain.ChainID)
	if err != nil {
		return nil, err
	}

	txHash, err := broadcast(ctx, c.active, txRaw)
	if err != nil {
		c.recordFailure(ctx, msgType, err)
		return nil, err
	}

	result, err := c.active.waitForConfirmation(ctx, txHash)
	if err != nil {
		return nil, err
	}
	c.recordSuccess(ctx, msgType, result.GasUsed, outputCount, estimate)
	return result, nil
}

func (c *Client) recordSuccess(ctx context.Context, msgType string, gasUsed uint64, outputCount int, est Estimate) {
	if c.opts.GasCache == nil || gasUsed == 0 {
		return
	}
	if err := c.opts.GasCache.RecordSuccess(
		ctx, c.chain.ChainID, msgType, gasUsed, outputCount, est.Fee.Denom, est.GasPriceAmount,
	); err != nil {
		slog.ErrorContext(ctx, "tx: record gas success", "chain", c.chain.ChainID, "error", err)
	}
}

func (c *Client) recordFailure(ctx context.Context, msgType string, err error) {
	if c.opts.GasCache == nil {
		return
	}
	reason := "broadcast_error"
	if IsInsufficientFee(err) {
		reason = "insufficient_fee"
	} else if errors.Is(err, ErrInsufficientGas) {
		reason = "out_of_gas"
	}
	if err := c.opts.GasCache.RecordFailure(ctx, c.chain.ChainID, msgType, reason); err != nil {
		slog.ErrorContext(ctx, "tx: record gas failure", "chain", c.chain.ChainID, "error", err)
	}
}
