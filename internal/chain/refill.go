package chain

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/observability"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// RefillLoop runs an immediate balance check, then repeats every c.refillInterval
// until ctx is canceled. Intended to be run as a goroutine from Chain.Start.
func (c *Chain) RefillLoop(ctx context.Context) {
	c.refillCheck(ctx)
	ticker := time.NewTicker(c.refillInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refillCheck(ctx)
		}
	}
}

// RefillNow synchronously tops up the distributor at keyIndex if its balance is
// below the threshold. Returns an error if the balance query or send fails.
// keyIndex must be in the range [1, len(distributorAddrs)].
func (c *Chain) RefillNow(ctx context.Context, keyIndex uint32) error {
	if keyIndex < 1 || int(keyIndex) > len(c.distributorAddrs) {
		return fmt.Errorf("chain: refill: key index %d out of range [1, %d]", keyIndex, len(c.distributorAddrs))
	}
	return c.refillOne(ctx, keyIndex, c.distributorAddrs[keyIndex-1])
}

func (c *Chain) refillCheck(ctx context.Context) {
	for i, addr := range c.distributorAddrs {
		if err := c.refillOne(ctx, uint32(i+1), addr); err != nil {
			c.log.Error("chain: refill check failed",
				"chain_id", c.info.ChainID, "key_index", i+1, "err", err)
		}
	}
}

func (c *Chain) refillOne(ctx context.Context, keyIndex uint32, addr string) error {
	bal, err := c.client.QueryBalance(ctx, addr, c.refillThreshold.Denom)
	if err != nil {
		return fmt.Errorf("query balance: %w", err)
	}

	if amt, parseErr := strconv.ParseFloat(bal.Amount, 64); parseErr == nil {
		observability.DistributorBalance.WithLabelValues(
			c.info.ChainID, fmt.Sprintf("%d", keyIndex), c.refillThreshold.Denom,
		).Set(amt)
	}

	topUp, needed := refillAmount(bal.Amount, c.refillThreshold.Amount)
	if !needed {
		return nil
	}

	_, err = c.client.BuildAndBroadcast(ctx, tx.SendRequest{
		KeyIndex:  0,
		ToAddress: addr,
		Coins:     tx.Coins{{Denom: c.refillThreshold.Denom, Amount: topUp}},
	})
	if err != nil {
		return fmt.Errorf("send refill to distributor %d: %w", keyIndex, err)
	}
	observability.DistributorRefillTotal.WithLabelValues(c.info.ChainID).Inc()
	c.log.Info("chain: distributor refilled",
		"chain_id", c.info.ChainID, "key_index", keyIndex,
		"amount", topUp, "denom", c.refillThreshold.Denom)
	return nil
}

// refillAmount returns the top-up amount and whether a refill is needed.
// Triggered when balance < threshold; fills to threshold × 10 to avoid
// refilling after every drip.
func refillAmount(balanceAmt, thresholdAmt string) (topUp string, needed bool) {
	bal := new(big.Int)
	thr := new(big.Int)
	bal.SetString(balanceAmt, 10)
	thr.SetString(thresholdAmt, 10)
	if bal.Cmp(thr) >= 0 {
		return "", false
	}
	target := new(big.Int).Mul(thr, big.NewInt(10))
	diff := new(big.Int).Sub(target, bal)
	return diff.String(), true
}
