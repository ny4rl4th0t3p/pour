package tx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// Options are optional parameters for New.
type Options struct {
	GasCache GasCache     // optional; read + write gas cache
	Logger   *slog.Logger // optional; defaults to slog.Default()
}

// Client is a single-chain tx client.
type Client struct {
	chain   *chainregistry.ChainInfo
	conn    *grpc.ClientConn
	txSvc   txv1beta1.ServiceClient
	authSvc authv1beta1.QueryClient
	opts    Options
}

// New dials the chain's first gRPC endpoint and returns a ready Client.
// Port 443 in the endpoint implies TLS; all other ports use plaintext.
func New(chain *chainregistry.ChainInfo, opts Options) (*Client, error) {
	if len(chain.Endpoints.GRPC) == 0 {
		return nil, fmt.Errorf("tx: chain %q: no gRPC endpoints configured", chain.ChainID)
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	endpoint := chain.Endpoints.GRPC[0].URL
	creds := grpc.WithTransportCredentials(insecure.NewCredentials())
	if strings.HasSuffix(endpoint, ":443") {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}))
	}

	conn, err := grpc.NewClient(endpoint, creds)
	if err != nil {
		return nil, fmt.Errorf("tx: dial %s: %w", endpoint, err)
	}

	return &Client{
		chain:   chain,
		conn:    conn,
		txSvc:   txv1beta1.NewServiceClient(conn),
		authSvc: authv1beta1.NewQueryClient(conn),
		opts:    opts,
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// BuildAndBroadcast derives the key at req.KeyIndex, builds a MsgSend, signs it, and
// broadcasts it. Records gas usage to Options.GasCache on success.
func (c *Client) BuildAndBroadcast(ctx context.Context, req SendRequest) (*BroadcastResult, error) {
	privKey, err := keys.DerivePrivKey(req.Mnemonic, c.chain.Slip44, req.KeyIndex)
	if err != nil {
		return nil, fmt.Errorf("tx: derive key: %w", err)
	}
	fromAddr, err := keys.AddressFromPubKey(privKey.PubKey(), c.chain.Bech32Prefix)
	if err != nil {
		return nil, fmt.Errorf("tx: derive address: %w", err)
	}

	msgAny, err := buildMsgSend(fromAddr, req.ToAddress, req.Coins)
	if err != nil {
		return nil, err
	}
	msgs := []*anypb.Any{msgAny}

	account, err := queryAccount(ctx, c.authSvc, fromAddr)
	if err != nil {
		return nil, err
	}

	estimate, err := estimateFee(ctx, c.txSvc, c.chain, msgs,
		feeOpts{GasCache: c.opts.GasCache, OutputCount: 1, MsgType: MsgTypeSend},
		c.opts.Logger)
	if err != nil {
		return nil, err
	}

	txRaw, err := buildTxRaw(privKey, *account, msgs, estimate, c.chain.ChainID)
	if err != nil {
		return nil, err
	}

	txHash, err := broadcast(ctx, c.txSvc, txRaw)
	if err != nil {
		c.recordFailure(ctx, MsgTypeSend, err)
		return nil, err
	}

	result, err := waitForConfirmation(ctx, c.txSvc, txHash)
	if err != nil {
		return nil, err
	}
	c.recordSuccess(ctx, MsgTypeSend, result.GasUsed, 1, estimate)
	return result, nil
}

// BuildAndBroadcastMulti derives the key at req.KeyIndex, builds a MsgMultiSend with
// all outputs, signs it, and broadcasts it. Records gas usage to Options.GasCache on success.
func (c *Client) BuildAndBroadcastMulti(ctx context.Context, req BatchSendRequest) (*BroadcastResult, error) {
	if len(req.Outputs) == 0 {
		return nil, fmt.Errorf("tx: BuildAndBroadcastMulti: outputs must not be empty")
	}

	privKey, err := keys.DerivePrivKey(req.Mnemonic, c.chain.Slip44, req.KeyIndex)
	if err != nil {
		return nil, fmt.Errorf("tx: derive key: %w", err)
	}
	fromAddr, err := keys.AddressFromPubKey(privKey.PubKey(), c.chain.Bech32Prefix)
	if err != nil {
		return nil, fmt.Errorf("tx: derive address: %w", err)
	}

	msgAny, err := buildMsgMultiSend(fromAddr, req.Outputs)
	if err != nil {
		return nil, err
	}
	msgs := []*anypb.Any{msgAny}

	account, err := queryAccount(ctx, c.authSvc, fromAddr)
	if err != nil {
		return nil, err
	}

	estimate, err := estimateFee(ctx, c.txSvc, c.chain, msgs,
		feeOpts{GasCache: c.opts.GasCache, OutputCount: len(req.Outputs), MsgType: MsgTypeMultiSend},
		c.opts.Logger)
	if err != nil {
		return nil, err
	}

	txRaw, err := buildTxRaw(privKey, *account, msgs, estimate, c.chain.ChainID)
	if err != nil {
		return nil, err
	}

	txHash, err := broadcast(ctx, c.txSvc, txRaw)
	if err != nil {
		c.recordFailure(ctx, MsgTypeMultiSend, err)
		return nil, err
	}

	result, err := waitForConfirmation(ctx, c.txSvc, txHash)
	if err != nil {
		return nil, err
	}
	c.recordSuccess(ctx, MsgTypeMultiSend, result.GasUsed, len(req.Outputs), estimate)
	return result, nil
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
