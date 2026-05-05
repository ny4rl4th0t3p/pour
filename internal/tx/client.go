package tx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	bankv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/bank/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// Options are optional parameters for New.
type Options struct {
	GasCache     GasCache      // optional; read + write gas cache
	Logger       *slog.Logger  // optional; defaults to slog.Default()
	EndpointPool *EndpointPool // optional; enables endpoint failover
}

// connBundle holds an active gRPC connection and its derived service clients.
type connBundle struct {
	url     string
	conn    *grpc.ClientConn
	txSvc   txv1beta1.ServiceClient
	authSvc authv1beta1.QueryClient
	bankSvc bankv1beta1.QueryClient
}

// Client is a single-chain tx client. All private keys are pre-derived at construction;
// the mnemonic is not retained after New returns.
type Client struct {
	chain      *chainregistry.ChainInfo
	bundle     connBundle
	pool       *EndpointPool
	cachedKeys map[uint32]*keys.PrivKey
	opts       Options
}

// New derives private keys for all keyIndices eagerly, then dials the first healthy
// endpoint. The mnemonic string is used only during New and is not stored in the
// returned Client.
func New(chain *chainregistry.ChainInfo, mnemonic string, keyIndices []uint32, opts Options) (*Client, error) {
	if len(chain.Endpoints.GRPC) == 0 {
		return nil, fmt.Errorf("tx: chain %q: no gRPC endpoints configured", chain.ChainID)
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

	var initialURL string
	if opts.EndpointPool != nil {
		u, ok := opts.EndpointPool.Next()
		if !ok {
			return nil, ErrNoEndpointAvailable
		}
		initialURL = u
	} else {
		initialURL = chain.Endpoints.GRPC[0].URL
	}

	b, err := newConnBundle(initialURL)
	if err != nil {
		return nil, fmt.Errorf("tx: dial %s: %w", initialURL, err)
	}

	return &Client{
		chain:      chain,
		bundle:     b,
		pool:       opts.EndpointPool,
		cachedKeys: cachedKeys,
		opts:       opts,
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	return c.bundle.conn.Close()
}

// QueryBalance returns the balance of denom for address.
func (c *Client) QueryBalance(ctx context.Context, address, denom string) (Coin, error) {
	resp, err := c.bundle.bankSvc.Balance(ctx, &bankv1beta1.QueryBalanceRequest{
		Address: address,
		Denom:   denom,
	})
	if err != nil {
		return Coin{}, fmt.Errorf("tx: query balance %s/%s: %w", address, denom, err)
	}
	if resp.Balance == nil {
		return Coin{Denom: denom, Amount: "0"}, nil
	}
	return Coin{Denom: resp.Balance.Denom, Amount: resp.Balance.Amount}, nil
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

// doSend runs the account-query → fee-estimate → sign → broadcast pipeline.
// On codes.Unavailable it switches to the next healthy endpoint and retries once.
func (c *Client) doSend(
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
		if !isUnavailable(err) || c.pool == nil {
			return nil, err
		}
		c.pool.MarkUnhealthy(c.bundle.url)
		nextURL, ok := c.pool.Next()
		if !ok {
			return nil, ErrNoEndpointAvailable
		}
		_ = c.bundle.conn.Close()
		b, dialErr := newConnBundle(nextURL)
		if dialErr != nil {
			return nil, fmt.Errorf("tx: failover dial %s: %w", nextURL, dialErr)
		}
		c.bundle = b
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
	account, err := queryAccount(ctx, c.bundle.authSvc, fromAddr)
	if err != nil {
		return nil, err
	}

	estimate, err := estimateFee(ctx, c.bundle.txSvc, c.chain, msgs,
		feeOpts{GasCache: c.opts.GasCache, OutputCount: outputCount, MsgType: msgType},
		c.opts.Logger)
	if err != nil {
		return nil, err
	}

	txRaw, err := buildTxRaw(privKey, *account, msgs, estimate, c.chain.ChainID)
	if err != nil {
		return nil, err
	}

	txHash, err := broadcast(ctx, c.bundle.txSvc, txRaw)
	if err != nil {
		c.recordFailure(ctx, msgType, err)
		return nil, err
	}

	result, err := waitForConfirmation(ctx, c.bundle.txSvc, txHash)
	if err != nil {
		return nil, err
	}
	c.recordSuccess(ctx, msgType, result.GasUsed, outputCount, estimate)
	return result, nil
}

// newConnBundle dials url and wraps the connection with service clients.
func newConnBundle(url string) (connBundle, error) {
	creds := grpc.WithTransportCredentials(insecure.NewCredentials())
	if endpointIsTLS(url) {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	conn, err := grpc.NewClient(url, creds)
	if err != nil {
		return connBundle{}, err
	}
	return connBundle{
		url:     url,
		conn:    conn,
		txSvc:   txv1beta1.NewServiceClient(conn),
		authSvc: authv1beta1.NewQueryClient(conn),
		bankSvc: bankv1beta1.NewQueryClient(conn),
	}, nil
}

// isUnavailable returns true if err or any error it wraps carries gRPC codes.Unavailable.
func isUnavailable(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if s, ok := status.FromError(e); ok && s.Code() == codes.Unavailable {
			return true
		}
	}
	return false
}

func (c *Client) recordSuccess(ctx context.Context, msgType string, gasUsed uint64, outputCount int, est Estimate) {
	if c.opts.GasCache == nil || gasUsed == 0 {
		return
	}
	_ = c.opts.GasCache.RecordSuccess(ctx, c.chain.ChainID, msgType, gasUsed, outputCount, est.Fee.Denom, est.GasPriceAmount)
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
	_ = c.opts.GasCache.RecordFailure(ctx, c.chain.ChainID, msgType, reason)
}
