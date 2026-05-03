package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/cosmos/go-bip39"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	pourhttp "github.com/ny4rl4th0t3p/pour/internal/http"
	"github.com/ny4rl4th0t3p/pour/internal/http/handlers"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type CLI struct {
	Serve   ServeCmd   `cmd:"" help:"Start the faucet daemon."`
	Keys    KeysCmd    `cmd:"" help:"Key management."`
	Version VersionCmd `cmd:"" help:"Print version information."`
}

// VersionCmd prints build metadata.
type VersionCmd struct{}

func (*VersionCmd) Run() error {
	fmt.Printf("pour %s (commit: %s, built: %s)\n", version, commit, date)
	return nil
}

// ServeCmd starts the faucet HTTP server.
type ServeCmd struct {
	config.ServeConfig
}

func (c *ServeCmd) Run() error {
	var level slog.LevelVar
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", c.LogLevel, err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: &level})))

	mnemonic := os.Getenv("POUR_MNEMONIC")
	if mnemonic == "" {
		return errors.New("POUR_MNEMONIC env var is required")
	}

	chains, err := config.LoadChains(c.ConfigFile)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	db, err := store.New(ctx, c.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	gc := gascache.New(db)
	gc.Start(ctx, lowGasPriceFn(chains))

	window, _ := chains.Abuse.IPRateLimit.WindowDuration()
	rpw := chains.Abuse.IPRateLimit.RequestsPerWindow
	if rpw == 0 {
		rpw = ratelimit.DefaultRequestsPerWindow
	}
	limiter := ratelimit.New(db, rpw, window)

	broadcasters := make(map[string]handlers.Broadcaster)
	var clients []*tx.Client
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for i := range chains.Chains {
		chain := chains.Chains[i]
		if !chain.Enabled {
			continue
		}
		if len(chain.Endpoints.GRPC) == 0 {
			return fmt.Errorf("chain %q: no gRPC endpoints configured", chain.ChainID)
		}
		client, err := tx.New(tx.ChainConfig{
			ChainID:      chain.ChainID,
			GRPCEndpoint: chain.Endpoints.GRPC[0],
			Bech32Prefix: chain.Bech32Prefix,
			Slip44:       chain.Slip44,
			FeeTokens:    makeFeeTokens(chain.FeeTokens),
		}, tx.Options{GasCache: gc})
		if err != nil {
			return fmt.Errorf("chain %q: tx client: %w", chain.ChainID, err)
		}
		clients = append(clients, client)
		broadcasters[chain.ChainID] = client
	}

	srv := pourhttp.New(pourhttp.Deps{
		ChainsConfig: chains,
		Serve:        &c.ServeConfig,
		Store:        db,
		Limiter:      limiter,
		Broadcasters: broadcasters,
		GasCache:     gc,
		Mnemonic:     mnemonic,
		Version:      version,
	})
	return srv.Start(ctx)
}

// KeysCmd groups key management subcommands.
type KeysCmd struct {
	Generate KeysGenerateCmd `cmd:"" help:"Generate a new BIP39 mnemonic."`
}

const mnemonicEntropyBits = 256

// KeysGenerateCmd prints a fresh 24-word BIP39 mnemonic.
type KeysGenerateCmd struct{}

func (*KeysGenerateCmd) Run() error {
	entropy, err := bip39.NewEntropy(mnemonicEntropyBits)
	if err != nil {
		return fmt.Errorf("entropy: %w", err)
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return fmt.Errorf("mnemonic: %w", err)
	}
	fmt.Println(mnemonic)
	return nil
}

func main() {
	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("pour"),
		kong.Description("A pure-Go, multi-chain Cosmos SDK faucet."),
		kong.UsageOnError(),
	)
	ctx.FatalIfErrorf(ctx.Run())
}

// lowGasPriceFn builds a gascache.LowGasPriceFn from the loaded config.
// It uses the first fee token's low_gas_price for each chain.
func lowGasPriceFn(cfg *config.ChainsConfig) gascache.LowGasPriceFn {
	m := make(map[string]string, len(cfg.Chains))
	for i := range cfg.Chains {
		c := cfg.Chains[i]
		if len(c.FeeTokens) > 0 {
			m[c.ChainID] = c.FeeTokens[0].LowGasPrice
		}
	}
	return func(chainID string) (string, bool) {
		v, ok := m[chainID]
		return v, ok && v != ""
	}
}

// makeFeeTokens converts config.FeeTokenConfig slice to tx.FeeToken slice.
func makeFeeTokens(in []config.FeeTokenConfig) []tx.FeeToken {
	out := make([]tx.FeeToken, len(in))
	for i, ft := range in {
		out[i] = tx.FeeToken{
			Denom:           ft.Denom,
			AverageGasPrice: ft.AverageGasPrice,
			LowGasPrice:     ft.LowGasPrice,
		}
	}
	return out
}
