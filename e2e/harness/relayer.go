package harness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
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

	// RelayerImage is the Hermes IBC relayer image used in e2e tests.
	RelayerImage = "ghcr.io/informalsystems/hermes:1.13.1"
)

// StartRelayer funds the relayer accounts on both chains, writes a Hermes config,
// starts the relayer container, performs the IBC handshake (creating channel-0 on both
// sides), and starts packet relaying. Cleanup terminates the container.
func StartRelayer(t *testing.T, ctx context.Context, chainA, chainB *SimappChain, networkName string) {
	t.Helper()

	fundRelayer(t, ctx, chainA, RelayerAddr)
	fundRelayer(t, ctx, chainB, RelayerAddr)

	configDir := writeHermesConfig(t, chainA, chainB)

	script := fmt.Sprintf(
		`hermes --config /home/hermes/config/config.toml keys add --chain %s --key-name relayer --mnemonic-file /home/hermes/config/mnemonic.txt &&`+
			` hermes --config /home/hermes/config/config.toml keys add --chain %s --key-name relayer --mnemonic-file /home/hermes/config/mnemonic.txt &&`+
			` hermes --config /home/hermes/config/config.toml create channel --a-chain %s --b-chain %s --a-port transfer --b-port transfer --new-client-connection --yes &&`+
			` hermes --config /home/hermes/config/config.toml start`,
		chainA.ChainID, chainB.ChainID, chainA.ChainID, chainB.ChainID,
	)

	req := testcontainers.ContainerRequest{
		Image:        RelayerImage,
		Entrypoint:   []string{"/bin/sh", "-c"},
		Cmd:          []string{script},
		ExposedPorts: []string{"3000/tcp"},
		Networks:     []string{networkName},
		Mounts: testcontainers.ContainerMounts{
			{
				Source: testcontainers.GenericBindMountSource{HostPath: configDir},
				Target: "/home/hermes/config",
			},
		},
		WaitingFor: wait.ForListeningPort("3000/tcp").WithStartupTimeout(5 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start relayer container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	// Stream Hermes container logs to stderr when POUR_E2E_VERBOSE is set.
	// We write to os.Stderr (not t.Log) to avoid panics if the goroutine outlives
	// the test function.
	if os.Getenv("POUR_E2E_VERBOSE") != "" {
		logReader, logErr := container.Logs(ctx)
		if logErr == nil {
			go func() {
				defer logReader.Close()
				scanner := bufio.NewScanner(logReader)
				for scanner.Scan() {
					fmt.Fprintf(os.Stderr, "[hermes] %s\n", scanner.Text())
				}
			}()
		}
	}

	// The IBC handshake completes before hermes start runs (port 3000 opens after).
	// Poll both chains until channel-0 is STATE_OPEN as a final consistency check.
	chainA.WaitForChannel(t, "channel-0", "transfer")
	chainB.WaitForChannel(t, "channel-0", "transfer")
}

// fundRelayer sends tokens from the validator account to the relayer address on the
// given chain, then polls until the balance is confirmed on-chain.
func fundRelayer(t *testing.T, ctx context.Context, chain *SimappChain, relayerAddr string) {
	t.Helper()
	cmd := []string{
		"simd", "tx", "bank", "send", "validator", relayerAddr,
		fmt.Sprintf("10000000%s", chain.cfg.Denom),
		"--keyring-backend", "test",
		"--chain-id", chain.cfg.ChainID,
		"--yes",
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		fmt.Sprintf("--gas-prices=0.025%s", chain.cfg.Denom),
		"--home", "/root/.simapp",
	}
	code, out, err := chain.ExecIn(ctx, cmd)
	if err != nil || code != 0 {
		output, _ := io.ReadAll(out)
		t.Fatalf("fund relayer on %s: exit %d: %v\n%s", chain.cfg.ChainID, code, err, output)
	}
	chain.WaitForBalance(t, relayerAddr, chain.cfg.Denom, 1)
}

// writeHermesConfig writes a Hermes TOML config and mnemonic file to a temp directory
// and returns the path to that directory (mounted as /home/hermes/config in the container).
func writeHermesConfig(t *testing.T, chainA, chainB *SimappChain) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "mnemonic.txt"), []byte(RelayerMnemonic), 0644); err != nil {
		t.Fatalf("write relayer mnemonic: %v", err)
	}

	cfg := fmt.Sprintf(`[global]
log_level = "info"

[mode.clients]
enabled = true
refresh = true
misbehaviour = true

[mode.connections]
enabled = true

[mode.channels]
enabled = true

[mode.packets]
enabled = true
clear_interval = 100
clear_on_start = true
tx_confirmation = false
auto_register_counterparty_payee = false

[rest]
enabled = true
host = "0.0.0.0"
port = 3000

[telemetry]
enabled = false
host = "0.0.0.0"
port = 3001

[[chains]]
id = %q
type = "CosmosSdk"
rpc_addr = "http://%s:26657"
grpc_addr = "http://%s:9090"
event_source = { mode = "pull", interval = "1s" }
rpc_timeout = "10s"
account_prefix = "cosmos"
key_name = "relayer"
store_prefix = "ibc"
default_gas = 100000
max_gas = 400000
gas_price = { price = 0.025, denom = %q }
gas_multiplier = 1.1
max_msg_num = 30
max_tx_size = 2097152
clock_drift = "5s"
max_block_time = "30s"
trusting_period = "14days"
trust_threshold = { numerator = "1", denominator = "3" }
address_type = { derivation = "cosmos" }

[[chains]]
id = %q
type = "CosmosSdk"
rpc_addr = "http://%s:26657"
grpc_addr = "http://%s:9090"
event_source = { mode = "pull", interval = "1s" }
rpc_timeout = "10s"
account_prefix = "cosmos"
key_name = "relayer"
store_prefix = "ibc"
default_gas = 100000
max_gas = 400000
gas_price = { price = 0.025, denom = %q }
gas_multiplier = 1.1
max_msg_num = 30
max_tx_size = 2097152
clock_drift = "5s"
max_block_time = "30s"
trusting_period = "14days"
trust_threshold = { numerator = "1", denominator = "3" }
address_type = { derivation = "cosmos" }
`,
		chainA.ChainID, chainA.ChainID, chainA.ChainID, chainA.GasDenom,
		chainB.ChainID, chainB.ChainID, chainB.ChainID, chainB.GasDenom,
	)

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write hermes config: %v", err)
	}

	// Hermes may write keys into the mounted dir; fix permissions so t.TempDir cleanup
	// succeeds. t.Cleanup is LIFO so the container terminates before this runs.
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, _ error) error {
			return os.Chmod(p, 0755)
		})
	})

	return dir
}
