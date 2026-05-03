package tx

import (
	"context"
	"crypto/tls"
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
)

// Options are optional parameters for New.
type Options struct {
	GasCache GasCache     // optional; implemented by internal/gascache in M5
	Logger   *slog.Logger // optional; defaults to slog.Default()
}

// Client is a single-chain tx client.
type Client struct {
	chain   ChainConfig
	conn    *grpc.ClientConn
	txSvc   txv1beta1.ServiceClient
	authSvc authv1beta1.QueryClient
	opts    Options
}

// New dials the chain's gRPC endpoint and returns a ready Client.
// Port 443 in the endpoint implies TLS; all other ports use plaintext.
func New(chain ChainConfig, opts Options) (*Client, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	creds := grpc.WithTransportCredentials(insecure.NewCredentials())
	if strings.HasSuffix(chain.GRPCEndpoint, ":443") {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}))
	}

	conn, err := grpc.NewClient(chain.GRPCEndpoint, creds)
	if err != nil {
		return nil, fmt.Errorf("tx: dial %s: %w", chain.GRPCEndpoint, err)
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

// BuildAndBroadcast derives the holder key, builds a MsgSend, signs it, and broadcasts it.
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

	estimate, err := estimateFee(ctx, c.txSvc, c.chain, msgs, req, c.opts.Logger)
	if err != nil {
		return nil, err
	}

	txRaw, err := buildTxRaw(privKey, *account, msgs, estimate, c.chain.ChainID)
	if err != nil {
		return nil, err
	}

	txHash, err := broadcast(ctx, c.txSvc, txRaw)
	if err != nil {
		return nil, err
	}

	return waitForConfirmation(ctx, c.txSvc, txHash)
}
