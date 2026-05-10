package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/pour/e2e/harness"
)

// TestIBCDiscovery validates that pour fetches IBC channel data from the registry
// and exposes it correctly via the API. v0.5.0 scope: discovery only, no actual transfers.
func TestIBCDiscovery(t *testing.T) {
	ctx := context.Background()

	chainA := harness.StartSimapp(t, ctx, harness.SimappConfigA)
	registryURL := harness.StartMockRegistry(t, chainA, nil)
	pour := harness.StartPour(t, harness.PourConfig{RegistryURL: registryURL})

	detail := pour.GetChainDetail(t, "simapp-a-1")
	require.Len(t, detail.IBCChannels, 1, "expected one IBC channel from mock registry")
	assert.Equal(t, "channel-0", detail.IBCChannels[0].ChannelID)
	assert.Equal(t, "simapp-b", detail.IBCChannels[0].PeerChainName)
	assert.Equal(t, "transfer", detail.IBCChannels[0].PortID)
	assert.Equal(t, "live", detail.IBCChannels[0].Status)
	assert.True(t, detail.IBCChannels[0].Preferred)

	info := pour.GetInfo(t)
	assert.Equal(t, 1, info.IBCChannelCount)
}

// TestIBCTransfer_HappyPath validates the full IBC drip path: pour receives a request
// for an IBC-destination chain (simapp-b-1), issues MsgTransfer on chain A, and the
// recipient receives the IBC-wrapped token on chain B.
func TestIBCTransfer_HappyPath(t *testing.T) {
	ctx := context.Background()

	netName := harness.CreateNetwork(t, ctx)

	cfgA := harness.SimappConfigA
	cfgA.NetworkName = netName
	chainA := harness.StartSimapp(t, ctx, cfgA)

	cfgB := harness.SimappConfigB
	cfgB.NetworkName = netName
	chainB := harness.StartSimapp(t, ctx, cfgB)

	harness.StartRelayer(t, ctx, chainA, chainB, netName)

	registryURL := harness.StartMockRegistry(t, chainA, chainB)
	pour := harness.StartPour(t, harness.PourConfig{RegistryURL: registryURL})

	resp := pour.Pour(t, "simapp-b-1", harness.TestRecipientAddr)
	require.Equal(t, "confirmed", resp.Status)
	assert.NotEmpty(t, resp.TxHash)

	ibcDenom := harness.IBCDenom("transfer", "channel-0", "stake")
	chainB.WaitForBalance(t, harness.TestRecipientAddr, ibcDenom, 1_000_000)
}
