package tx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
)

// transport abstracts the wire calls needed by the send pipeline.
// grpcTransport and restTransport implement it.
type transport interface {
	queryAccount(ctx context.Context, address string) (*Account, error)
	// simulate returns (0, nil) when simulation is unavailable — the caller
	// falls through to the next fee estimation strategy.
	simulate(ctx context.Context, txBytes []byte) (gasUsed uint64, err error)
	broadcastTx(ctx context.Context, txBytes []byte) (txHash string, err error)
	waitForConfirmation(ctx context.Context, txHash string) (*BroadcastResult, error)
	queryBalance(ctx context.Context, address, denom string) (Coin, error)
	endpointURL() string
	close() error
}

// Timing variables shared by grpcTransport and restTransport. Exposed as vars
// so tests can override them without sleeping.
var (
	confirmTimeout      = 30 * time.Second
	confirmPollInterval = 2 * time.Second
)

// broadcast marshals txRaw and submits it via the transport.
func broadcast(ctx context.Context, t transport, txRaw *txv1beta1.TxRaw) (string, error) {
	txBytes, err := proto.Marshal(txRaw)
	if err != nil {
		return "", fmt.Errorf("tx: marshal TxRaw: %w", err)
	}
	return t.broadcastTx(ctx, txBytes)
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

// isUnavailable reports whether err signals a transport-level failure on either
// the gRPC path (codes.Unavailable) or the REST path (errRESTUnavailable).
func isUnavailable(err error) bool {
	if errors.Is(err, errRESTUnavailable) {
		return true
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if s, ok := status.FromError(e); ok && s.Code() == codes.Unavailable {
			return true
		}
	}
	return false
}
