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

	chainA := harness.StartChainA(t, ctx)
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
