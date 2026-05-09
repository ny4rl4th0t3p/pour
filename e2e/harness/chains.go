package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// SimappImage is the official cosmos/simapp Docker image.
	// Pin to a specific tag for reproducibility; verify genesis CLI on first run.
	SimappImage = "ghcr.io/cosmos/simapp:v0.53"
)

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
	SimappConfigB = SimappConfig{ChainID: "simapp-b-1", ChainName: "simapp-b", Denom: "uosmo", Prefix: "osmo"}
)

// SimappChain holds the host-accessible endpoints of a running simapp container.
type SimappChain struct {
	GRPCAddr   string // "host:port" — external, for pour config
	RESTAddr   string // "host:port" — external, cosmos API server (port 1317)
	InternalIP string // Docker bridge IP — for relayer config
	cfg        SimappConfig
	container  testcontainers.Container
}

// StartSimapp boots a parameterised simapp chain. The gRPC port is waited on before
// returning. Cleanup terminates the container.
func StartSimapp(t *testing.T, ctx context.Context, cfg SimappConfig) *SimappChain {
	t.Helper()

	const home = "/root/.simapp"
	initScript := fmt.Sprintf(`
		simd init test --chain-id %[2]s --home %[1]s &&
		simd keys add validator --keyring-backend test --home %[1]s &&
		simd genesis add-genesis-account \
		  $(simd keys show validator -a --keyring-backend test --home %[1]s) \
		  10000000000%[3]s --home %[1]s &&
		simd genesis gentx validator 1000000%[3]s \
		  --chain-id %[2]s --keyring-backend test --home %[1]s &&
		simd genesis collect-gentxs --home %[1]s &&
		simd start --home %[1]s \
		  --rpc.laddr tcp://0.0.0.0:26657 \
		  --grpc.address 0.0.0.0:9090 \
		  --api.enable --api.address tcp://0.0.0.0:1317 \
		  --minimum-gas-prices 0.025%[3]s
	`, home, cfg.ChainID, cfg.Denom)

	req := testcontainers.ContainerRequest{
		Image:        SimappImage,
		ExposedPorts: []string{"9090/tcp", "26657/tcp", "1317/tcp"},
		Cmd:          []string{"sh", "-c", initScript},
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
	internalIP, err := container.ContainerIP(ctx)
	if err != nil {
		t.Fatalf("%s: get internal IP: %v", cfg.ChainID, err)
	}

	return &SimappChain{
		GRPCAddr:   host + ":" + grpcPort.Port(),
		RESTAddr:   host + ":" + restPort.Port(),
		InternalIP: internalIP,
		cfg:        cfg,
		container:  container,
	}
}

// StartChainA boots a single simapp chain using SimappConfigA. Kept for backward
// compatibility with existing tests.
func StartChainA(t *testing.T, ctx context.Context) *SimappChain {
	return StartSimapp(t, ctx, SimappConfigA)
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
