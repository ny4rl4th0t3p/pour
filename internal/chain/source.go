package chain

import (
	"context"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// ChainSnapshot is a point-in-time view of a single active chain.
type ChainSnapshot struct {
	Info       *chainregistry.ChainInfo
	Drip       chainregistry.DripPolicy
	IBCTimeout time.Duration
	// IBCDrips lists IBC drip configurations for this chain. Each entry defines a
	// token that can be sent to recipients on this chain via MsgTransfer from a
	// source chain's wallet. Empty for native-only chains.
	IBCDrips []config.IBCDripConfig
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
