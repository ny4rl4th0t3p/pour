package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SimappImage is the ibc-go simapp image — the cosmos/simapp image does not wire
// IBC into its gRPC router; ibc-go-simd does. Binary and genesis CLI are identical.
const SimappImage = "ghcr.io/cosmos/ibc-go-simd:v8.5.2"

// SimappConfig parameterises a simapp chain for use in e2e tests.
type SimappConfig struct {
	ChainID      string
	ChainName    string
	Denom        string
	Prefix       string
	NetworkName  string // optional; attach container to this Docker network
	HostHomePath string // optional; bind-mounted as /root/.simapp so genesis is readable on host
	Restartable  bool   // if true, simd runs in a loop so ResetChain can trigger a chain reset
}

var (
	// HubConfig is the IBC hub chain — holds tokens and fires MsgTransfer.
	HubConfig = SimappConfig{ChainID: "hub-1", ChainName: "hub", Denom: "stake", Prefix: "cosmos"}
	// MyNetConfig is the operator's destination chain — receives IBC vouchers.
	// ibc-go-simd v8.5.2 (Cosmos SDK v0.50.x) always uses the "cosmos" bech32 prefix
	// regardless of the chain's native gas denom. Both test chains share the same prefix.
	MyNetConfig = SimappConfig{ChainID: "mynet-1", ChainName: "mynet", Denom: "uosmo", Prefix: "cosmos"}
	// StandaloneConfig is the operator's chain in auto-mode tests — no IBC, stake denom
	// kept so auto-mode drip assertions match without duplicating genesis complexity.
	StandaloneConfig = SimappConfig{ChainID: "mynet-1", ChainName: "mynet", Denom: "stake", Prefix: "cosmos"}
)

// SimappChain holds the host-accessible endpoints of a running simapp container.
type SimappChain struct {
	GRPCAddr     string // "host:port" — external, for pour config
	RESTAddr     string // "host:port" — external, cosmos API server (port 1317)
	RPCAddr      string // "http://host:port" — Tendermint RPC (port 26657)
	ChainID      string // chain ID, also used as Docker DNS hostname on custom networks
	GasDenom     string // native gas denom (e.g. "stake", "uosmo")
	HostHomePath string // host-side path of the bind-mounted /root/.simapp (if configured)
	cfg          SimappConfig
	container    testcontainers.Container
}

// StartSimapp boots a parameterised simapp chain. The gRPC port is waited on before
// returning. Cleanup terminates the container.
func StartSimapp(t *testing.T, ctx context.Context, cfg SimappConfig) *SimappChain {
	t.Helper()

	const home = "/root/.simapp"

	// ibc-go-simd v8.5.2 hardcodes "stake" as the bond denom regardless of the
	// chain's native gas token. When cfg.Denom != "stake" (e.g. chain B uses "uosmo"),
	// fund the validator with both denoms so it can bond AND pay gas.
	validatorGenCoins := "10000000000stake"
	if cfg.Denom != "stake" {
		validatorGenCoins += ",10000000000" + cfg.Denom
	}
	faucetGenCoins := "1000000000stake"
	if cfg.Denom != "stake" {
		faucetGenCoins += ",1000000000" + cfg.Denom
	}

	startCmd := fmt.Sprintf(
		`simd start --home %[1]s --rpc.laddr tcp://0.0.0.0:26657 --grpc.address 0.0.0.0:9090 --api.enable --api.address tcp://0.0.0.0:1317 --minimum-gas-prices 0.025%[2]s`,
		home, cfg.Denom,
	)
	if cfg.Restartable {
		startCmd = fmt.Sprintf(`while true; do
			%s &
			echo $! > /tmp/simd.pid
			wait $!
			simd comet unsafe-reset-all --home %s 2>/dev/null || simd tendermint unsafe-reset-all --home %s 2>/dev/null || true
			sleep 1
		done`, startCmd, home, home)
	}

	initScript := fmt.Sprintf(`
		simd init test --chain-id %[2]s --home %[1]s &&
		simd keys add validator --keyring-backend test --home %[1]s &&
		simd genesis add-genesis-account \
		  $(simd keys show validator -a --keyring-backend test --home %[1]s) \
		  %[3]s --home %[1]s &&
		echo %[5]q | simd keys add pour-faucet --recover --keyring-backend test --home %[1]s &&
		simd genesis add-genesis-account \
		  $(simd keys show pour-faucet -a --keyring-backend test --home %[1]s) \
		  %[4]s --home %[1]s &&
		simd genesis gentx validator 1000000stake \
		  --chain-id %[2]s --keyring-backend test --home %[1]s &&
		simd genesis collect-gentxs --home %[1]s &&
		%[6]s
	`, home, cfg.ChainID, validatorGenCoins, faucetGenCoins, TestMnemonic, startCmd)

	req := testcontainers.ContainerRequest{
		Image:        SimappImage,
		Hostname:     cfg.ChainID, // Docker DNS name on custom networks, used by Hermes
		ExposedPorts: []string{"9090/tcp", "26657/tcp", "1317/tcp"},
		Entrypoint:   []string{"sh", "-c"},
		Cmd:          []string{initScript},
		WaitingFor:   wait.ForListeningPort("9090/tcp").WithStartupTimeout(90 * time.Second),
	}
	if cfg.NetworkName != "" {
		req.Networks = []string{cfg.NetworkName}
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start %s container: %v", cfg.ChainID, err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("%s: get host: %v", cfg.ChainID, err)
	}
	grpcPort, err := container.MappedPort(ctx, "9090/tcp")
	if err != nil {
		t.Fatalf("%s: get grpc port: %v", cfg.ChainID, err)
	}
	rpcPort, err := container.MappedPort(ctx, "26657/tcp")
	if err != nil {
		t.Fatalf("%s: get rpc port: %v", cfg.ChainID, err)
	}
	restPort, err := container.MappedPort(ctx, "1317/tcp")
	if err != nil {
		t.Fatalf("%s: get rest port: %v", cfg.ChainID, err)
	}
	restAddr := host + ":" + restPort.Port()
	waitForFirstBlock(t, restAddr, cfg.ChainID)

	if cfg.HostHomePath != "" {
		if err := os.MkdirAll(filepath.Join(cfg.HostHomePath, "config"), 0755); err != nil {
			t.Fatalf("%s: mkdir config: %v", cfg.ChainID, err)
		}
		rc, err := container.CopyFileFromContainer(ctx, home+"/config/genesis.json")
		if err != nil {
			t.Fatalf("%s: copy genesis.json: %v", cfg.ChainID, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("%s: read genesis.json: %v", cfg.ChainID, err)
		}
		if err := os.WriteFile(filepath.Join(cfg.HostHomePath, "config", "genesis.json"), data, 0644); err != nil {
			t.Fatalf("%s: write genesis.json: %v", cfg.ChainID, err)
		}
	}

	return &SimappChain{
		GRPCAddr:     host + ":" + grpcPort.Port(),
		RPCAddr:      "http://" + host + ":" + rpcPort.Port(),
		RESTAddr:     restAddr,
		ChainID:      cfg.ChainID,
		GasDenom:     cfg.Denom,
		HostHomePath: cfg.HostHomePath,
		cfg:          cfg,
		container:    container,
	}
}

// waitForFirstBlock polls the chain's REST API until at least one block has been
// committed. The gRPC port can open before the first block is committed, so without
// this guard ExecIn calls (e.g. fundRelayer) fail with "invalid height".
func waitForFirstBlock(t *testing.T, restAddr, chainID string) {
	t.Helper()
	waitForBlockHeight(t, restAddr, chainID, 1)
}

// waitForBlockHeight polls the chain's REST API until the latest committed block
// is at or above minHeight.
func waitForBlockHeight(t *testing.T, restAddr, chainID string, minHeight int64) {
	t.Helper()
	url := fmt.Sprintf("http://%s/cosmos/base/tendermint/v1beta1/blocks/latest", restAddr)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil {
			var body struct {
				Block struct {
					Header struct {
						Height string `json:"height"`
					} `json:"header"`
				} `json:"block"`
			}
			if jsonErr := json.NewDecoder(resp.Body).Decode(&body); jsonErr == nil {
				if h, parseErr := strconv.ParseInt(body.Block.Header.Height, 10, 64); parseErr == nil && h >= minHeight {
					resp.Body.Close()
					return
				}
			}
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s: did not reach block height %d within 90s", chainID, minHeight)
}

// WaitForBlockHeight blocks until the chain's latest committed block is at or
// above minHeight. Use before ResetChain to ensure the watcher has recorded a
// meaningful prior height.
func (s *SimappChain) WaitForBlockHeight(t *testing.T, minHeight int64) {
	t.Helper()
	waitForBlockHeight(t, s.RESTAddr, s.ChainID, minHeight)
}

// ExecIn runs a command inside the container.
func (s *SimappChain) ExecIn(ctx context.Context, cmd []string) (int, io.Reader, error) {
	return s.container.Exec(ctx, cmd)
}

// ResetChain kills the running simd process, so the restartable loop resets chain state
// and restarts simd from height 0. Blocks until the chain has produced at least 2 blocks,
// confirming it is actively running and not just at genesis. Only valid when cfg.Restartable is true.
func (s *SimappChain) ResetChain(t *testing.T, ctx context.Context) {
	t.Helper()
	if !s.cfg.Restartable {
		t.Fatal("ResetChain requires Restartable: true in SimappConfig")
	}
	if _, _, err := s.container.Exec(ctx, []string{"sh", "-c", "kill $(cat /tmp/simd.pid)"}); err != nil {
		t.Fatalf("ResetChain %s: kill simd: %v", s.ChainID, err)
	}
	waitForBlockHeight(t, s.RESTAddr, s.ChainID, 3)
}

// SendTokens sends amount of the chain's gas denom from the validator account to toAddr,
// then waits until toAddr holds at least 1 token.
func (s *SimappChain) SendTokens(t *testing.T, ctx context.Context, toAddr, amount string) {
	t.Helper()
	cmd := []string{
		"simd", "tx", "bank", "send", "validator", toAddr, amount,
		"--keyring-backend", "test",
		"--chain-id", s.ChainID,
		"--yes",
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		fmt.Sprintf("--gas-prices=0.025%s", s.GasDenom),
		"--home", "/root/.simapp",
	}
	code, out, err := s.ExecIn(ctx, cmd)
	if err != nil || code != 0 {
		output, _ := io.ReadAll(out)
		t.Fatalf("SendTokens on %s: exit %d: %v\n%s", s.ChainID, code, err, output)
	}
	s.WaitForBalance(t, toAddr, s.GasDenom, 1)
}

// QueryBalance returns the amount of denom held by address according to the chain's
// REST API, or 0 if the balance cannot be determined.
func (s *SimappChain) QueryBalance(t *testing.T, address, denom string) int64 {
	t.Helper()
	url := fmt.Sprintf("http://%s/cosmos/bank/v1beta1/balances/%s", s.RESTAddr, address)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var body struct {
		Balances []struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"balances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0
	}
	for _, b := range body.Balances {
		if b.Denom == denom {
			n, _ := strconv.ParseInt(b.Amount, 10, 64)
			return n
		}
	}
	return 0
}

// WaitForBalance polls QueryBalance every 2s until address holds at least minAmount
// of denom, or fails the test after 60s.
func (s *SimappChain) WaitForBalance(t *testing.T, address, denom string, minAmount int64) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if s.QueryBalance(t, address, denom) >= minAmount {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("WaitForBalance: %s on %s never reached %d within 60s", denom, s.cfg.ChainID, minAmount)
}

// IBCDenom computes the IBC denomination for a token transferred over a single hop.
// Result is "ibc/" + uppercase hex SHA256 of "<portID>/<channelID>/<baseDenom>".
func IBCDenom(portID, channelID, baseDenom string) string {
	path := portID + "/" + channelID + "/" + baseDenom
	hash := sha256.Sum256([]byte(path))
	return "ibc/" + strings.ToUpper(hex.EncodeToString(hash[:]))
}

// WaitForChannel polls the chain's REST API until channelID on portID is in STATE_OPEN,
// or fails the test after 120 s. Used after StartRelayer as a consistency check: the
// IBC handshake runs before hermes start, but the channel state may not yet be visible
// via REST when the container becomes ready.
func (s *SimappChain) WaitForChannel(t *testing.T, channelID, portID string) {
	t.Helper()
	url := fmt.Sprintf("http://%s/ibc/core/channel/v1/channels", s.RESTAddr)
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if ibcChannelIsOpen(url, channelID, portID) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("WaitForChannel: %s/%s not STATE_OPEN on %s within 120s", portID, channelID, s.cfg.ChainID)
}

func ibcChannelIsOpen(url, channelID, portID string) bool {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		Channels []struct {
			ChannelID string `json:"channel_id"`
			PortID    string `json:"port_id"`
			State     string `json:"state"`
		} `json:"channels"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return false
	}
	for _, ch := range body.Channels {
		if ch.ChannelID == channelID && ch.PortID == portID && ch.State == "STATE_OPEN" {
			return true
		}
	}
	return false
}

// CreateNetwork creates a Docker bridge network for the test and registers cleanup.
// Returns the network name to pass into SimappConfig.NetworkName and StartRelayer.
func CreateNetwork(t *testing.T, ctx context.Context) string {
	t.Helper()
	name := fmt.Sprintf("pour-e2e-%d", time.Now().UnixNano())
	net, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name:           name,
			CheckDuplicate: true,
		},
	})
	if err != nil {
		t.Fatalf("create Docker network: %v", err)
	}
	t.Cleanup(func() { _ = net.Remove(ctx) })
	return name
}
