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
	"github.com/ny4rl4th0t3p/pour/internal/admin"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	pourhttp "github.com/ny4rl4th0t3p/pour/internal/http"
	"github.com/ny4rl4th0t3p/pour/internal/http/handlers"
	"github.com/ny4rl4th0t3p/pour/internal/store"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type CLI struct {
	Serve   ServeCmd   `cmd:"" help:"Start the faucet daemon."`
	Chains  ChainsCmd  `cmd:"" help:"Chain registry management."`
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
	lgpFn, err := lowGasPriceFn(chains)
	if err != nil {
		return err
	}
	gc.Start(ctx, lgpFn)

	window, _ := chains.Abuse.IPRateLimit.WindowDuration()
	rpw := chains.Abuse.IPRateLimit.RequestsPerWindow
	if rpw == 0 {
		rpw = ratelimit.DefaultRequestsPerWindow
	}
	limiter := ratelimit.New(db, rpw, window)

	refreshInterval, err := chains.Registry.RefreshDuration()
	if err != nil {
		return err
	}

	mgr, err := chain.New(ctx, chain.Options{
		Config:          chains,
		GasCache:        gc,
		RegistryBaseURL: chains.Registry.BaseURL,
		RefreshInterval: refreshInterval,
	})
	if err != nil {
		return err
	}
	defer mgr.Close()
	mgr.StartRefreshLoop(ctx)

	rawClients := mgr.Clients()
	broadcasters := make(map[string]handlers.Broadcaster, len(rawClients))
	for id, c := range rawClients {
		broadcasters[id] = c
	}

	tokenStore, err := admin.NewTokenStore()
	if err != nil {
		return err
	}
	adminHandler := admin.New(admin.Deps{
		RegStore:   mgr.Store(),
		Manager:    mgr,
		ConfigPath: c.ConfigFile,
	})
	adminRouter := admin.Middleware(tokenStore, chains.Admin.AllowedCIDRs)(adminHandler.Router())

	srv, err := pourhttp.New(pourhttp.Deps{
		Manager:         mgr,
		RefreshInterval: refreshInterval,
		Serve:           &c.ServeConfig,
		Store:           db,
		Limiter:         limiter,
		Broadcasters:    broadcasters,
		GasCache:        gc,
		AdminHandler:    adminRouter,
		Mnemonic:        mnemonic,
		Version:         version,
	})
	if err != nil {
		return err
	}
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
func lowGasPriceFn(cfg *config.ChainsConfig) (gascache.LowGasPriceFn, error) {
	m := make(map[string]string, len(cfg.Chains))
	for i := range cfg.Chains {
		c := &cfg.Chains[i]
		info, err := c.ToChainInfo()
		if err != nil {
			return nil, err
		}
		if len(info.FeeTokens) > 0 && !info.FeeTokens[0].LowGasPrice.IsZero() {
			m[c.ChainID] = info.FeeTokens[0].LowGasPrice.String()
		}
	}
	return func(chainID string) (string, bool) {
		v, ok := m[chainID]
		return v, ok && v != ""
	}, nil
}
