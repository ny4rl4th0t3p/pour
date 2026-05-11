package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMnemonic is the canonical BIP39 test mnemonic used across the Cosmos ecosystem.
// Never commit a real mnemonic.
const TestMnemonic = "test test test test test test test test test test test junk"

// TestRecipientAddr is the cosmos-prefix bech32 address derived from TestMnemonic at
// m/44'/118'/0'/0/0. It is the address pour uses to sign transactions (seeded in
// genesis as the pour-faucet account) and the recipient in IBC transfer e2e tests.
const TestRecipientAddr = "cosmos15yk64u7zc9g9k2yr2wmzeva5qgwxps6yxj00e7"

// PourConfig parameterises a StartPour call.
type PourConfig struct {
	RegistryURL string
}

// PourServer holds the running pour process and its base URL.
type PourServer struct {
	BaseURL string
	cmd     *exec.Cmd
}

// StartPour resolves the pour binary, writes a chains.yml, starts the process, and
// polls /health until it returns 200. Cleanup kills the process.
func StartPour(t *testing.T, cfg PourConfig) *PourServer {
	t.Helper()

	bin := resolveBin(t)
	dir := t.TempDir()
	writeChainsYML(t, dir, cfg.RegistryURL)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = append(os.Environ(),
		"POUR_MNEMONIC="+TestMnemonic,
		"POUR_LISTEN=127.0.0.1:18080",
		"POUR_CONFIG="+filepath.Join(dir, "chains.yml"),
		"POUR_DB_PATH="+filepath.Join(dir, "pour.db"),
		"POUR_LOG_LEVEL=debug",
	)
	cmd.Stdout = &testWriter{t: t, prefix: "[pour] "}
	cmd.Stderr = &testWriter{t: t, prefix: "[pour] "}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pour: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	waitForHealth(t, "http://127.0.0.1:18080/health")
	return &PourServer{BaseURL: "http://127.0.0.1:18080", cmd: cmd}
}

// Pour calls POST /v1/pour and decodes the response.
func (s *PourServer) Pour(t *testing.T, chainID, address string) PourResponse {
	t.Helper()
	body := fmt.Sprintf(`{"chain_id":%q,"address":%q}`, chainID, address)
	resp, err := http.Post(s.BaseURL+"/v1/pour", "application/json", strings.NewReader(body)) //nolint:noctx
	if err != nil {
		t.Fatalf("POST /v1/pour: %v", err)
	}
	defer resp.Body.Close()
	var out PourResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode pour response: %v", err)
	}
	return out
}

// GetChainDetail calls GET /v1/chains/{chainID} and decodes the response.
func (s *PourServer) GetChainDetail(t *testing.T, chainID string) ChainDetailResponse {
	t.Helper()
	resp, err := http.Get(s.BaseURL + "/v1/chains/" + chainID)
	if err != nil {
		t.Fatalf("GET /v1/chains/%s: %v", chainID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/chains/%s: status %d", chainID, resp.StatusCode)
	}
	var out ChainDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode chain detail: %v", err)
	}
	return out
}

// GetInfo calls GET /v1/info and decodes the response.
func (s *PourServer) GetInfo(t *testing.T) InfoResponse {
	t.Helper()
	resp, err := http.Get(s.BaseURL + "/v1/info")
	if err != nil {
		t.Fatalf("GET /v1/info: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/info: status %d", resp.StatusCode)
	}
	var out InfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	return out
}

// PourAutoConfig parameterises a StartPourAuto call.
type PourAutoConfig struct {
	HomePath     string // host dir containing config/genesis.json (from bind mount)
	RPCAddr      string // Tendermint RPC URL, e.g. "http://127.0.0.1:26657"
	GRPCAddr     string // gRPC endpoint, e.g. "127.0.0.1:9090"
	FundMnemonic string // optional; triggers self-funding via POUR_FUND_MNEMONIC
	PourMnemonic string // pre-written to $HOME/.pour/auto-mnemonic before pour starts
}

// StartPourAuto starts pour in --auto mode. It pre-writes PourMnemonic to an isolated
// HOME directory so the distributor address is deterministic, then waits for /health.
func StartPourAuto(t *testing.T, cfg PourAutoConfig) *PourServer {
	t.Helper()

	bin := resolveBin(t)
	homeDir := t.TempDir() // pour's HOME; isolates the auto-mnemonic file per test

	if cfg.PourMnemonic != "" {
		pourDir := filepath.Join(homeDir, ".pour")
		if err := os.MkdirAll(pourDir, 0700); err != nil {
			t.Fatalf("StartPourAuto: mkdir .pour: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pourDir, "auto-mnemonic"), []byte(cfg.PourMnemonic+"\n"), 0600); err != nil {
			t.Fatalf("StartPourAuto: write auto-mnemonic: %v", err)
		}
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	args := []string{
		"serve",
		"--auto",
		"--home", cfg.HomePath,
		"--rpc", cfg.RPCAddr,
		"--grpc", cfg.GRPCAddr,
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
		"POUR_LISTEN="+listenAddr,
		"POUR_DB_PATH="+filepath.Join(homeDir, "pour.db"),
		"POUR_LOG_LEVEL=debug",
	)
	if cfg.FundMnemonic != "" {
		cmd.Env = append(cmd.Env, "POUR_FUND_MNEMONIC="+cfg.FundMnemonic)
	}
	cmd.Stdout = &testWriter{t: t, prefix: "[pour-auto] "}
	cmd.Stderr = &testWriter{t: t, prefix: "[pour-auto] "}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pour (auto): %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	waitForHealth(t, "http://"+listenAddr+"/health")
	return &PourServer{BaseURL: "http://" + listenAddr, cmd: cmd}
}

// freePort returns an available TCP port on 127.0.0.1 by briefly binding to :0.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func resolveBin(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("POUR_BIN"); b != "" {
		if _, err := os.Stat(b); err == nil {
			return b
		}
		t.Fatalf("POUR_BIN=%q: file not found", b)
	}
	if v := os.Getenv("POUR_VERSION"); v != "" {
		pkg := "github.com/ny4rl4th0t3p/pour/cmd/pour@" + v
		if out, err := exec.Command("go", "install", pkg).CombinedOutput(); err != nil {
			t.Fatalf("go install %s: %v\n%s", pkg, err, out)
		}
		gopath, err := exec.Command("go", "env", "GOPATH").Output()
		if err != nil {
			t.Fatalf("go env GOPATH: %v", err)
		}
		return filepath.Join(strings.TrimSpace(string(gopath)), "bin", "pour")
	}
	t.Fatal("POUR_BIN or POUR_VERSION must be set")
	return ""
}

// writeChainsYML writes a minimal chains.yml with both simapp chains.
// Both chain IDs must be present so pour computes the simapp-a/simapp-b pair
// and fetches the _IBC/ file from the mock registry.
func writeChainsYML(t *testing.T, dir, registryURL string) {
	t.Helper()
	content := fmt.Sprintf(`registry:
  base_url: %q
  refresh_interval: "1h"

chains:
  - chain_id: simapp-a-1
    enabled: true
    drip:
      anonymous: "1000000stake"
      max_per_address_per_day: "10000000stake"
    ibc:
      timeout: "30s"
  - chain_id: simapp-b-1
    enabled: true
    drip:
      anonymous: "1000000stake"
      max_per_address_per_day: "10000000stake"
    ibc:
      source_chain_id: simapp-a-1
      timeout: "30s"
`, registryURL)
	if err := os.WriteFile(filepath.Join(dir, "chains.yml"), []byte(content), 0600); err != nil {
		t.Fatalf("write chains.yml: %v", err)
	}
}

func waitForHealth(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
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
	t.Fatalf("pour did not become healthy at %s within 30s", url)
}

type testWriter struct {
	t      *testing.T
	prefix string
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Log(w.prefix + strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
