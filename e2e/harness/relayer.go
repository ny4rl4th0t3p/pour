package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// RelayerMnemonic is the standard all-zeros BIP39 test mnemonic.
	// Distinct from TestMnemonic to avoid sequence nonce collisions with pour.
	RelayerMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	// RelayerImage is the go-relayer image used in e2e tests.
	RelayerImage = "ghcr.io/cosmos/relayer:v2.5.2"

	// Bech32 addresses derived from RelayerMnemonic at m/44'/118'/0'/0/0.
	// Verified against the keys test vectors in internal/tx/internal/keys/derive_test.go.
	relayerAddrA = "cosmos19rl4cm2hmr8afy4kldpxz3fka4jguq0auqdal4"
	relayerAddrB = "osmo19rl4cm2hmr8afy4kldpxz3fka4jguq0a5m7df8"
)

// StartRelayer funds the relayer accounts on both chains, writes a go-relayer config,
// starts the relayer container, performs the IBC handshake (creating channel-0 on both
// sides), and starts packet relaying. Cleanup terminates the container.
func StartRelayer(t *testing.T, ctx context.Context, chainA, chainB *SimappChain, networkName string) {
	t.Helper()

	fundRelayer(t, ctx, chainA, relayerAddrA)
	fundRelayer(t, ctx, chainB, relayerAddrB)

	configDir := writeRelayerConfig(t, chainA, chainB)

	script := fmt.Sprintf(
		`rly keys restore simapp-a relayer %q &&`+
			` rly keys restore simapp-b relayer %q &&`+
			` rly tx link a-b --src-port transfer --dst-port transfer &&`+
			` rly start a-b`,
		RelayerMnemonic, RelayerMnemonic,
	)

	req := testcontainers.ContainerRequest{
		Image:        RelayerImage,
		Entrypoint:   []string{"/bin/sh", "-c"},
		Cmd:          []string{script},
		ExposedPorts: []string{"5183/tcp"},
		Networks:     []string{networkName},
		Mounts: testcontainers.ContainerMounts{
			{
				Source: testcontainers.GenericBindMountSource{HostPath: configDir},
				Target: "/home/relayer/.relayer",
			},
		},
		WaitingFor: wait.ForListeningPort("5183/tcp").WithStartupTimeout(5 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start relayer container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })
}

// fundRelayer sends tokens from the validator account to the relayer address on the
// given chain, then waits 3s for the tx to be included.
func fundRelayer(t *testing.T, ctx context.Context, chain *SimappChain, relayerAddr string) {
	t.Helper()
	cmd := []string{
		"simd", "tx", "bank", "send", "validator", relayerAddr,
		fmt.Sprintf("10000000%s", chain.cfg.Denom),
		"--keyring-backend", "test",
		"--chain-id", chain.cfg.ChainID,
		"--yes",
		fmt.Sprintf("--fees=1000%s", chain.cfg.Denom),
		"--home", "/root/.simapp",
	}
	code, _, err := chain.ExecIn(ctx, cmd)
	if err != nil || code != 0 {
		t.Fatalf("fund relayer on %s: exit %d: %v", chain.cfg.ChainID, code, err)
	}
	time.Sleep(3 * time.Second)
}

// writeRelayerConfig writes the go-relayer config.yaml to a temp directory and
// returns the path to that directory (to be mounted as /home/relayer/.relayer).
func writeRelayerConfig(t *testing.T, chainA, chainB *SimappChain) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0700); err != nil {
		t.Fatalf("mkdir relayer config: %v", err)
	}

	cfg := fmt.Sprintf(`global:
  api-listen-addr: :5183
  timeout: 20s
chains:
  simapp-a:
    type: cosmos
    value:
      key: relayer
      chain-id: simapp-a-1
      rpc-addr: "http://%s:26657"
      grpc-addr: "http://%s:9090"
      account-prefix: cosmos
      keyring-backend: test
      gas-adjustment: 1.3
      gas-prices: 0.025stake
      debug: false
      timeout: 20s
      sign-mode: direct
  simapp-b:
    type: cosmos
    value:
      key: relayer
      chain-id: simapp-b-1
      rpc-addr: "http://%s:26657"
      grpc-addr: "http://%s:9090"
      account-prefix: osmo
      keyring-backend: test
      gas-adjustment: 1.3
      gas-prices: 0.025uosmo
      debug: false
      timeout: 20s
      sign-mode: direct
paths:
  a-b:
    src:
      chain-id: simapp-a-1
    dst:
      chain-id: simapp-b-1
`,
		chainA.InternalIP, chainA.InternalIP,
		chainB.InternalIP, chainB.InternalIP,
	)

	path := filepath.Join(dir, "config", "config.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatalf("write relayer config: %v", err)
	}
	return dir
}
