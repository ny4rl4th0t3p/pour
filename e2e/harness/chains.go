package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	ChainID     string
	ChainName   string
	Denom       string
	Prefix      string
	NetworkName string // optional; attach container to this Docker network
}

var (
	SimappConfigA = SimappConfig{ChainID: "simapp-a-1", ChainName: "simapp-a", Denom: "stake", Prefix: "cosmos"}
	// ibc-go-simd v8.5.2 (Cosmos SDK v0.50.x) always uses the "cosmos" bech32 prefix
	// regardless of the chain's native gas denom. Both test chains share the same prefix.
	SimappConfigB = SimappConfig{ChainID: "simapp-b-1", ChainName: "simapp-b", Denom: "uosmo", Prefix: "cosmos"}
)

// SimappChain holds the host-accessible endpoints of a running simapp container.
type SimappChain struct {
	GRPCAddr  string // "host:port" — external, for pour config
	RESTAddr  string // "host:port" — external, cosmos API server (port 1317)
	ChainID   string // chain ID, also used as Docker DNS hostname on custom networks
	GasDenom  string // native gas denom (e.g. "stake", "uosmo")
	cfg       SimappConfig
	container testcontainers.Container
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
		simd start --home %[1]s \
		  --rpc.laddr tcp://0.0.0.0:26657 \
		  --grpc.address 0.0.0.0:9090 \
		  --api.enable --api.address tcp://0.0.0.0:1317 \
		  --minimum-gas-prices 0.025%[6]s
	`, home, cfg.ChainID, validatorGenCoins, faucetGenCoins, TestMnemonic, cfg.Denom)

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
	restPort, err := container.MappedPort(ctx, "1317/tcp")
	if err != nil {
		t.Fatalf("%s: get rest port: %v", cfg.ChainID, err)
	}
	restAddr := host + ":" + restPort.Port()
	waitForFirstBlock(t, restAddr, cfg.ChainID)

	return &SimappChain{
		GRPCAddr:  host + ":" + grpcPort.Port(),
		RESTAddr:  restAddr,
		ChainID:   cfg.ChainID,
		GasDenom:  cfg.Denom,
		cfg:       cfg,
		container: container,
	}
}

// waitForFirstBlock polls the chain's REST API until at least one block has been
// committed. The gRPC port can open before the first block is committed, so without
// this guard ExecIn calls (e.g. fundRelayer) fail with "invalid height".
func waitForFirstBlock(t *testing.T, restAddr, chainID string) {
	t.Helper()
	url := fmt.Sprintf("http://%s/cosmos/base/tendermint/v1beta1/blocks/latest", restAddr)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s: no block produced within 60s", chainID)
}

// ExecIn runs a command inside the container.
func (s *SimappChain) ExecIn(ctx context.Context, cmd []string) (int, io.Reader, error) {
	return s.container.Exec(ctx, cmd)
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
