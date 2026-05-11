package tx

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	bankv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/bank/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
)

// grpcTransport holds an active gRPC connection and implements transport.
type grpcTransport struct {
	url     string
	conn    *grpc.ClientConn
	txSvc   txv1beta1.ServiceClient
	authSvc authv1beta1.QueryClient
	bankSvc bankv1beta1.QueryClient
}

// newGRPCTransport dials url and wraps the connection with service clients.
func newGRPCTransport(url string) (*grpcTransport, error) {
	creds := grpc.WithTransportCredentials(insecure.NewCredentials())
	if endpointIsTLS(url) {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	conn, err := grpc.NewClient(url, creds)
	if err != nil {
		return nil, err
	}
	return &grpcTransport{
		url:     url,
		conn:    conn,
		txSvc:   txv1beta1.NewServiceClient(conn),
		authSvc: authv1beta1.NewQueryClient(conn),
		bankSvc: bankv1beta1.NewQueryClient(conn),
	}, nil
}

func (g *grpcTransport) endpointURL() string { return g.url }
func (g *grpcTransport) close() error        { return g.conn.Close() }

func (g *grpcTransport) queryAccount(ctx context.Context, address string) (*Account, error) {
	return queryAccountGRPC(ctx, g.authSvc, address)
}

func (g *grpcTransport) simulate(ctx context.Context, txBytes []byte) (uint64, error) {
	simResp, err := g.txSvc.Simulate(ctx, &txv1beta1.SimulateRequest{TxBytes: txBytes})
	if err != nil || simResp.GasInfo == nil {
		return 0, nil //nolint:nilerr // simulation is optional; 0 means fall through
	}
	return simResp.GasInfo.GasUsed, nil
}

func (g *grpcTransport) broadcastTx(ctx context.Context, txBytes []byte) (string, error) {
	resp, err := g.txSvc.BroadcastTx(ctx, &txv1beta1.BroadcastTxRequest{
		TxBytes: txBytes,
		Mode:    txv1beta1.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	if err != nil {
		return "", fmt.Errorf("tx: broadcast: %w", err)
	}
	txResp := resp.GetTxResponse()
	if txResp == nil {
		return "", fmt.Errorf("tx: broadcast: nil response")
	}
	if txResp.Code != 0 {
		return "", classifyChainError(txResp)
	}
	return txResp.Txhash, nil
}

func (g *grpcTransport) waitForConfirmation(ctx context.Context, txHash string) (*BroadcastResult, error) {
	deadline := time.Now().Add(confirmTimeout)
	for {
		if time.Now().After(deadline) {
			return nil, ErrBroadcastTimeout
		}

		resp, err := g.txSvc.GetTx(ctx, &txv1beta1.GetTxRequest{Hash: txHash})
		if err != nil {
			if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
				if !sleepOrCancel(ctx, confirmPollInterval) {
					return nil, ctx.Err()
				}
				continue
			}
			// Transient error; retry.
			if !sleepOrCancel(ctx, confirmPollInterval) {
				return nil, ctx.Err()
			}
			continue
		}

		txResp := resp.GetTxResponse()
		if txResp == nil {
			return nil, fmt.Errorf("tx: confirmation: nil tx response")
		}
		if txResp.Code != 0 {
			return nil, classifyChainError(txResp)
		}
		var gasUsed uint64
		if txResp.GasUsed > 0 {
			gasUsed = uint64(txResp.GasUsed)
		}
		return &BroadcastResult{TxHash: txResp.Txhash, Height: txResp.Height, GasUsed: gasUsed}, nil
	}
}

func (g *grpcTransport) queryBalance(ctx context.Context, address, denom string) (Coin, error) {
	resp, err := g.bankSvc.Balance(ctx, &bankv1beta1.QueryBalanceRequest{
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
