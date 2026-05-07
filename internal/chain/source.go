package chain

import (
	"context"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// ChainSnapshot is a point-in-time view of a single active chain.
type ChainSnapshot struct {
	Info             *chainregistry.ChainInfo
	Drip             chainregistry.DripPolicy
	IBCTimeout       time.Duration
	IBCSourceChainID string // empty for native chains
}

// ChainSource is the interface HTTP handlers use to access active chain data.
// *Manager implements this interface.
type ChainSource interface {
	GetActive(chainID string) (ChainSnapshot, bool)
	ListActive() []ChainSnapshot
	LastFetched() time.Time
	PendingFrozenCount() int
	ChannelsFor(chainName string) []chainregistry.IBCChannel
	AllIBCChannels() []chainregistry.IBCChannel
	IBCTransfer(ctx context.Context, sourceChainID string, req tx.TransferRequest) (tx.TransferResult, error)
}
