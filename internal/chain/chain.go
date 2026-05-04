package chain

import (
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// Chain is an active, connected chain managed by Manager.
type Chain struct {
	info   *chainregistry.ChainInfo
	drip   chainregistry.DripPolicy
	client *tx.Client
}

func newChain(info *chainregistry.ChainInfo, drip chainregistry.DripPolicy, gc tx.GasCache) (*Chain, error) {
	client, err := tx.New(info, tx.Options{GasCache: gc})
	if err != nil {
		return nil, err
	}
	return &Chain{info: info, drip: drip, client: client}, nil
}

// Info returns the chain's resolved configuration.
func (c *Chain) Info() *chainregistry.ChainInfo { return c.info }

// Drip returns the drip policy for this chain.
func (c *Chain) Drip() chainregistry.DripPolicy { return c.drip }

// Client returns the tx.Client for broadcasting transactions.
func (c *Chain) Client() *tx.Client { return c.client }

// Close closes the underlying gRPC connection.
func (c *Chain) Close() { _ = c.client.Close() }
