package harness

import (
	"context"
	"fmt"
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

// SimappChain holds the host-accessible endpoints of a running simapp container.
type SimappChain struct {
	GRPCAddr string // host:mapped-port, e.g. "localhost:54321"
}

// StartChainA boots a single simapp chain (chain-id simapp-a-1, denom uatom).
// Genesis is initialised inside the container via a shell one-liner; the gRPC
// port is waited on before returning. Cleanup terminates the container.
func StartChainA(t *testing.T, ctx context.Context) *SimappChain {
	t.Helper()

	// ghcr.io/cosmos/simapp runs as root; chain home is /root/.simapp.
	const home = "/root/.simapp"
	initScript := fmt.Sprintf(`
		simd init test --chain-id simapp-a-1 --home %[1]s &&
		simd keys add validator --keyring-backend test --home %[1]s &&
		simd genesis add-genesis-account \
		  $(simd keys show validator -a --keyring-backend test --home %[1]s) \
		  10000000000uatom --home %[1]s &&
		simd genesis gentx validator 1000000uatom \
		  --chain-id simapp-a-1 --keyring-backend test --home %[1]s &&
		simd genesis collect-gentxs --home %[1]s &&
		simd start --home %[1]s \
		  --rpc.laddr tcp://0.0.0.0:26657 \
		  --grpc.address 0.0.0.0:9090 \
		  --minimum-gas-prices 0.025uatom
	`, home)

	req := testcontainers.ContainerRequest{
		Image:        SimappImage,
		ExposedPorts: []string{"9090/tcp"},
		Cmd:          []string{"sh", "-c", initScript},
		WaitingFor:   wait.ForListeningPort("9090/tcp").WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start simapp-a container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("simapp-a: get host: %v", err)
	}
	port, err := container.MappedPort(ctx, "9090/tcp")
	if err != nil {
		t.Fatalf("simapp-a: get mapped port: %v", err)
	}

	return &SimappChain{GRPCAddr: host + ":" + port.Port()}
}
