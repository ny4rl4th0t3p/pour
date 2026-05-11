package e2e_test

import (
	"context"
	"testing"
	"time"

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

// TestAutoMode_HappyPath validates the full --auto mode path: genesis is parsed from the
// bind-mounted home dir, the pour address self-funds from the genesis funder account, and
// a drip to a fresh recipient succeeds.
func TestAutoMode_HappyPath(t *testing.T) {
	ctx := context.Background()

	cfg := harness.SimappConfigA
	cfg.HostHomePath = t.TempDir()
	simapp := harness.StartSimapp(t, ctx, cfg)

	pour := harness.StartPourAuto(t, harness.PourAutoConfig{
		HomePath:     cfg.HostHomePath,
		RPCAddr:      simapp.RPCAddr,
		GRPCAddr:     simapp.GRPCAddr,
		FundMnemonic: harness.TestMnemonic,
		PourMnemonic: harness.TestMnemonic,
	})

	resp := pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
	require.Equal(t, "confirmed", resp.Status)
	assert.NotEmpty(t, resp.TxHash)

	simapp.WaitForBalance(t, harness.TestAutoRecipient, "stake", 1)
}

// TestAutoMode_WaitForFunding validates the flow where no fund-mnemonic is provided:
// pour polls until an external actor funds its address, then begins serving requests.
func TestAutoMode_WaitForFunding(t *testing.T) {
	ctx := context.Background()

	cfg := harness.SimappConfigA
	cfg.HostHomePath = t.TempDir()
	simapp := harness.StartSimapp(t, ctx, cfg)

	// Fund pour's address (derived from RelayerMnemonic, not in genesis) concurrently.
	// pour.StartPourAuto blocks on /health until pour detects the balance and starts.
	go func() {
		time.Sleep(3 * time.Second)
		_, _, _ = simapp.ExecIn(ctx, []string{
			"simd", "tx", "bank", "send", "validator", harness.RelayerAddr, "5000000stake",
			"--keyring-backend", "test",
			"--chain-id", simapp.ChainID,
			"--yes", "--gas", "auto", "--gas-adjustment", "1.3",
			"--gas-prices", "0.025stake",
			"--home", "/root/.simapp",
		})
	}()

	pour := harness.StartPourAuto(t, harness.PourAutoConfig{
		HomePath:     cfg.HostHomePath,
		RPCAddr:      simapp.RPCAddr,
		GRPCAddr:     simapp.GRPCAddr,
		PourMnemonic: harness.RelayerMnemonic,
	})

	resp := pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
	require.Equal(t, "confirmed", resp.Status)
	assert.NotEmpty(t, resp.TxHash)
}

// TestAutoMode_HotReload validates that pour recovers automatically after a devnet chain
// reset: the block height regression is detected, the gRPC client is reconnected, and
// subsequent drips succeed without operator intervention.
func TestAutoMode_HotReload(t *testing.T) {
	ctx := context.Background()

	cfg := harness.SimappConfigA
	cfg.HostHomePath = t.TempDir()
	cfg.Restartable = true
	simapp := harness.StartSimapp(t, ctx, cfg)

	pour := harness.StartPourAuto(t, harness.PourAutoConfig{
		HomePath:     cfg.HostHomePath,
		RPCAddr:      simapp.RPCAddr,
		GRPCAddr:     simapp.GRPCAddr,
		FundMnemonic: harness.TestMnemonic,
		PourMnemonic: harness.TestMnemonic,
	})

	// Baseline drip — confirms chain and pour are working before the reset.
	resp := pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
	require.Equal(t, "confirmed", resp.Status, "baseline drip before reset")

	// Wait for a height well above what the reset chain can produce within one watcher
	// poll interval (5 s at ~1 block/s). This guarantees the watcher has recorded a
	// prevHeight that the restarted chain cannot reach before the next poll fires,
	// ensuring the height regression is detected.
	simapp.WaitForBlockHeight(t, 15)
	simapp.ResetChain(t, ctx)

	// Poll until pour detects the height regression, reconnects, and serves drips again.
	deadline := time.Now().Add(30 * time.Second)
	var lastResp harness.PourResponse
	for time.Now().Before(deadline) {
		lastResp = pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
		if lastResp.Status == "confirmed" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	require.Equal(t, "confirmed", lastResp.Status, "drip after chain reset and reconnect")
}

// TestAutoMode_GRPCToRESTFailover validates that pour automatically falls over to REST
// when the active gRPC endpoint goes down mid-session. Pour is started with both
// endpoints; a baseline drip confirms gRPC is working; then the gRPC proxy is closed to
// simulate an endpoint failure; the subsequent drip must still confirm via REST.
func TestAutoMode_GRPCToRESTFailover(t *testing.T) {
	ctx := context.Background()

	cfg := harness.SimappConfigA
	cfg.HostHomePath = t.TempDir()
	simapp := harness.StartSimapp(t, ctx, cfg)

	// Route gRPC through a local proxy so we can kill it independently of simapp.
	proxy := harness.StartTCPProxy(t, simapp.GRPCAddr)

	pour := harness.StartPourAuto(t, harness.PourAutoConfig{
		HomePath:     cfg.HostHomePath,
		RPCAddr:      simapp.RPCAddr,
		GRPCAddr:     proxy.Addr(),
		RESTAddr:     "http://" + simapp.RESTAddr,
		FundMnemonic: harness.TestMnemonic,
		PourMnemonic: harness.TestMnemonic,
	})

	// Baseline: gRPC proxy is up, drip should succeed via gRPC.
	resp := pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
	require.Equal(t, "confirmed", resp.Status, "baseline drip before gRPC failure")

	// Kill the proxy — gRPC connections from pour drop immediately.
	proxy.Close()

	// Pour must detect the gRPC failure, fall over to REST, and still serve drips.
	resp = pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
	require.Equal(t, "confirmed", resp.Status, "drip after gRPC→REST failover")
}

// TestAutoMode_RESTOnly validates that pour works end-to-end when the chain is
// configured with only a REST/LCD endpoint (no gRPC). The tx client uses the REST
// transport for all wire operations: account query, simulate, broadcast, and confirmation.
func TestAutoMode_RESTOnly(t *testing.T) {
	ctx := context.Background()

	cfg := harness.SimappConfigA
	cfg.HostHomePath = t.TempDir()
	simapp := harness.StartSimapp(t, ctx, cfg)

	pour := harness.StartPourAuto(t, harness.PourAutoConfig{
		HomePath:     cfg.HostHomePath,
		RPCAddr:      simapp.RPCAddr,
		GRPCAddr:     "", // REST-only: override the default
		RESTAddr:     "http://" + simapp.RESTAddr,
		FundMnemonic: harness.TestMnemonic,
		PourMnemonic: harness.TestMnemonic,
	})

	resp := pour.Pour(t, "simapp-a-1", harness.TestAutoRecipient)
	require.Equal(t, "confirmed", resp.Status)
	assert.NotEmpty(t, resp.TxHash)

	simapp.WaitForBalance(t, harness.TestAutoRecipient, "stake", 1)
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
