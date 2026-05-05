package tx

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
)

var (
	confirmTimeout      = 30 * time.Second
	confirmPollInterval = 2 * time.Second
)

// broadcast marshals txRaw and submits it via BROADCAST_MODE_SYNC.
func broadcast(ctx context.Context, txSvc txv1beta1.ServiceClient, txRaw *txv1beta1.TxRaw) (string, error) {
	txBytes, err := proto.Marshal(txRaw)
	if err != nil {
		return "", fmt.Errorf("tx: marshal TxRaw: %w", err)
	}

	resp, err := txSvc.BroadcastTx(ctx, &txv1beta1.BroadcastTxRequest{
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

// waitForConfirmation polls GetTx until the tx is included in a block or the deadline is reached.
// Transient gRPC errors are retried; a NotFound response means the tx is still in the mempool.
func waitForConfirmation(ctx context.Context, txSvc txv1beta1.ServiceClient, txHash string) (*BroadcastResult, error) {
	deadline := time.Now().Add(confirmTimeout)
	for {
		if time.Now().After(deadline) {
			return nil, ErrBroadcastTimeout
		}

		resp, err := txSvc.GetTx(ctx, &txv1beta1.GetTxRequest{Hash: txHash})
		if err != nil {
			if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
				// Tx not yet in a block; keep polling.
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
		return &BroadcastResult{TxHash: txResp.Txhash, Height: txResp.Height, GasUsed: uint64(txResp.GasUsed)}, nil
	}
}

// sleepOrCancel sleeps for d or returns false if ctx is canceled first.
func sleepOrCancel(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}
