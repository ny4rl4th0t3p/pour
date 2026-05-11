package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/cosmos/go-bip39"

	"github.com/ny4rl4th0t3p/pour/internal/abuse"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/apikey"
	abusepow "github.com/ny4rl4th0t3p/pour/internal/abuse/pow"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/signed"
	"github.com/ny4rl4th0t3p/pour/internal/admin"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/devnet"
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

	// Auto-configure mode: derive everything from a running local chain.
	// Companion flags (--home etc.) are hidden from main help; see README for usage.
	Auto         bool   `kong:"help='Auto-configure from a running local chain. See --home, --rpc, --grpc, --rest, --drip, --fund-mnemonic.'"`
	Home         string `kong:"hidden,help='Chain home directory (e.g. ~/.simapp). Required with --auto.'"`
	RPC          string `kong:"hidden,default='http://localhost:26657',help='Tendermint RPC address for --auto mode.'"`
	GRPC         string `kong:"hidden,default='localhost:9090',help='gRPC address for --auto mode. Use empty string for REST-only.'"`
	REST         string `kong:"hidden,help='REST/LCD address for --auto mode (e.g. http://localhost:1317). Omit to use gRPC only.'"`
	Drip         string `kong:"hidden,help='Drip amount in --auto mode (e.g. 1000000uatom). Default: 1000000<denom>.'"`
	FundMnemonic string `kong:"hidden,env='POUR_FUND_MNEMONIC',help='Mnemonic of a funded genesis account for self-funding on startup.'"`
}

// powAdapter adapts *abusepow.Issuer to handlers.PowIssuer with a fixed difficulty.
type powAdapter struct {
	issuer     *abusepow.Issuer
	difficulty abusepow.Difficulty
}

func (a *powAdapter) NewChallenge() (string, error) {
	return a.issuer.NewChallenge(a.difficulty)
}

// buildAutoConfig builds an in-memory ChainsConfig from the chain's genesis file
// and returns a MnemonicFn that reads the mnemonic from disk on each call.
// The mnemonic value is never captured in the closure — only the path is.
func (c *ServeCmd) buildAutoConfig() (mnemonicFn func() string, chains *config.ChainsConfig, err error) {
	if c.Home == "" {
		return nil, nil, errors.New("--home is required when using --auto (e.g. --home ~/.simapp)")
	}

	info, err := devnet.ParseGenesis(c.Home)
	if err != nil {
		return nil, nil, err
	}
	slog.Info("devnet: genesis parsed", "chain_id", info.ChainID, "prefix", info.Bech32Prefix, "denom", info.NativeDenom)

	mnemonicPath, err := devnet.DefaultMnemonicPath()
	if err != nil {
		return nil, nil, err
	}
	// Ensure the file exists (generate if absent) at startup; capture only the path.
	if _, err = devnet.LoadOrGenerate(mnemonicPath); err != nil {
		return nil, nil, err
	}

	chains, err = devnet.BuildConfig(info, c.GRPC, c.REST, c.Drip)
	if err != nil {
		return nil, nil, err
	}
	mnemonicFn = func() string {
		m, loadErr := devnet.LoadOrGenerate(mnemonicPath)
		if loadErr != nil {
			slog.Error("devnet: reload mnemonic", "error", loadErr)
			return ""
		}
		return m
	}
	return mnemonicFn, chains, nil
}

func (c *ServeCmd) Run() error {
	var level slog.LevelVar
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", c.LogLevel, err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: &level})))

	var mnemonicFn func() string
	var chains *config.ChainsConfig

	if c.Auto {
		var err error
		mnemonicFn, chains, err = c.buildAutoConfig()
		if err != nil {
			return err
		}
	} else {
		if os.Getenv("POUR_MNEMONIC") == "" {
			return errors.New("POUR_MNEMONIC env var is required")
		}
		mnemonicFn = func() string { return os.Getenv("POUR_MNEMONIC") }
		var err error
		chains, err = config.LoadChains(c.ConfigFile)
		if err != nil {
			return err
		}
	}

	logAbuseWarnings(chains)

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

	refreshInterval, err := chains.Registry.RefreshDuration()
	if err != nil {
		return err
	}

	mgr, err := chain.New(ctx, chain.Options{
		Config:          chains,
		GasCache:        gc,
		MnemonicFn:      mnemonicFn,
		RegistryBaseURL: chains.Registry.BaseURL,
		RefreshInterval: refreshInterval,
	})
	if err != nil {
		return err
	}
	defer mgr.Close()
	mgr.Start(ctx)
	mgr.StartRefreshLoop(ctx)

	rawClients := mgr.Clients()

	if c.Auto && len(chains.Chains) > 0 {
		if err := runAutoFunding(ctx, mgr, rawClients, &chains.Chains[0], c.FundMnemonic); err != nil {
			return err
		}
		mgr.StartDevnetWatcher(ctx, chains.Chains[0].ChainID, c.RPC)
	}

	broadcasters := buildBroadcasters(mgr)

	tokenStore, err := admin.NewTokenStore()
	if err != nil {
		return err
	}

	ac := buildAbuseComponents(chains, db, tokenStore.HMACKey, rawClients, mgr.ListActive(), buildLimiter(chains.Abuse, db))

	adminHandler := admin.New(admin.Deps{
		RegStore:    mgr.Store(),
		Manager:     mgr,
		GasCache:    gc,
		ConfigPath:  c.ConfigFile,
		APIKeyStore: ac.apiKeyStore,
		TokenStore:  tokenStore,
	})
	adminRouter := admin.Middleware(tokenStore, chains.Admin.AllowedCIDRs)(adminHandler.Router())

	srv, err := pourhttp.New(pourhttp.Deps{
		Manager:         mgr,
		RefreshInterval: refreshInterval,
		Serve:           &c.ServeConfig,
		Store:           db,
		Gate:            ac.gate,
		PowIssuer:       &powAdapter{issuer: ac.powIssuer, difficulty: ac.powDifficulty},
		NonceIssuer:     ac.nonceStore,
		AbuseCfg:        chains.Abuse,
		Broadcasters:    broadcasters,
		AdminHandler:    adminRouter,
		Version:         version,
	})
	if err != nil {
		return err
	}
	return srv.Start(ctx)
}

// abuseComponents groups the gate and its collaborating issuers so buildAbuseComponents
// can return them as a unit without a long return list.
type abuseComponents struct {
	gate          *abuse.Gate
	powIssuer     *abusepow.Issuer
	powDifficulty abusepow.Difficulty
	nonceStore    *signed.NonceStore
	apiKeyStore   *apikey.Store
}

func buildBroadcasters(mgr *chain.Manager) map[string]handlers.Broadcaster {
	active := mgr.ListActive()
	out := make(map[string]handlers.Broadcaster, len(active))
	for _, snap := range active {
		out[snap.Info.ChainID] = &dynamicBroadcaster{mgr: mgr, chainID: snap.Info.ChainID}
	}
	return out
}

// dynamicBroadcaster looks up the current tx.Client from the manager on every
// call so that a chain reset (which replaces the client) is transparent to callers.
type dynamicBroadcaster struct {
	mgr     *chain.Manager
	chainID string
}

func (d *dynamicBroadcaster) BuildAndBroadcast(ctx context.Context, req tx.SendRequest) (*tx.BroadcastResult, error) {
	c, ok := d.mgr.GetChain(d.chainID)
	if !ok {
		return nil, fmt.Errorf("chain %q: not found", d.chainID)
	}
	cl := c.Client()
	if cl == nil {
		return nil, fmt.Errorf("chain %q: no tx client (IBC-destination chain)", d.chainID)
	}
	return cl.BuildAndBroadcast(ctx, req)
}

func buildLimiter(ab config.AbuseConfig, db *store.Store) *ratelimit.Limiter {
	window, _ := ab.IPRateLimit.WindowDuration()
	rpw := ab.IPRateLimit.RequestsPerWindow
	if rpw == 0 {
		rpw = ratelimit.DefaultRequestsPerWindow
	}
	return ratelimit.New(db, rpw, window)
}

func buildAbuseComponents(
	cfg *config.ChainsConfig,
	db *store.Store,
	hmacKeyFn func() []byte,
	rawClients map[string]*tx.Client,
	snapshots []chain.ChainSnapshot,
	limiter *ratelimit.Limiter,
) abuseComponents {
	powIssuer := abusepow.New(hmacKeyFn)
	powDifficulty := abusepow.DifficultyMedium
	if d := cfg.Abuse.PoW.Difficulty; d != "" {
		if parsed, parseErr := abusepow.ParseDifficulty(d); parseErr == nil {
			powDifficulty = parsed
		}
	}
	nonceStore := signed.NewNonceStore(5 * time.Minute)
	sigVerifier := signed.New()
	queriers := make(map[string]signed.BalanceQuerier, len(rawClients))
	for id, cl := range rawClients {
		queriers[id] = cl
	}
	prefixes := make(map[string]string, len(snapshots))
	for _, snap := range snapshots {
		prefixes[snap.Info.ChainID] = snap.Info.Bech32Prefix
	}
	predicateChecker := signed.NewPredicateChecker(queriers, prefixes)
	apiKeyStore := apikey.New(db)
	gate := abuse.New(cfg.Abuse, apiKeyStore, powIssuer, nonceStore, sigVerifier, predicateChecker, limiter)
	return abuseComponents{
		gate:          gate,
		powIssuer:     powIssuer,
		powDifficulty: powDifficulty,
		nonceStore:    nonceStore,
		apiKeyStore:   apiKeyStore,
	}
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

const ipRateLimitWarnThreshold = 10000

// logAbuseWarnings emits slog.Warn messages for operator-visible misconfigurations (§6.8).
func logAbuseWarnings(cfg *config.ChainsConfig) {
	ab := cfg.Abuse
	if !ab.PoW.Enabled && !ab.APIKeys.Enabled && !ab.SignatureChallenge.Enabled {
		slog.Warn("abuse: all mechanisms disabled — faucet is open to the public with no authentication")
	}
	if ab.IPRateLimit.RequestsPerWindow > ipRateLimitWarnThreshold {
		slog.Warn("abuse: ip_rate_limit.requests_per_window is very high",
			"value", ab.IPRateLimit.RequestsPerWindow)
	}
	predicateChainID := ab.SignatureChallenge.PredicateChainID
	for i := range cfg.Chains {
		ch := &cfg.Chains[i]
		if predicateChainID != "" && ch.ChainID == predicateChainID && ch.IsEnabled() {
			slog.Warn("abuse: chain used as predicate_chain_id should not be enabled — "+
				"enabling it opens a drip endpoint with no faucet funds",
				"chain_id", ch.ChainID)
		}
		if !ch.IsEnabled() {
			continue
		}
		if ab.SignatureChallenge.Enabled && ch.Drip.Signed == "" {
			slog.Warn("abuse: signature_challenge enabled but drip.signed not configured for chain",
				"chain_id", ch.ChainID)
		}
		if signedDripRatioHigh(ch.Drip.Signed, ch.Drip.Anonymous) {
			slog.Warn("abuse: drip.signed is more than 100x drip.anonymous",
				"chain_id", ch.ChainID,
				"signed", ch.Drip.Signed,
				"anonymous", ch.Drip.Anonymous)
		}
	}
}

// signedDripRatioHigh reports whether the signed drip amount is more than 100× the anonymous
// drip amount. Returns false when either value is absent, unparseable, or denoms differ.
func signedDripRatioHigh(signedCoin, anonCoin string) bool {
	if signedCoin == "" || anonCoin == "" {
		return false
	}
	s, errS := config.ParseCoin(signedCoin)
	a, errA := config.ParseCoin(anonCoin)
	if errS != nil || errA != nil || s.Denom != a.Denom {
		return false
	}
	sv, ok1 := new(big.Int).SetString(s.Amount, 10)
	av, ok2 := new(big.Int).SetString(a.Amount, 10)
	if !ok1 || !ok2 || av.Sign() <= 0 {
		return false
	}
	return sv.Cmp(new(big.Int).Mul(av, big.NewInt(100))) > 0
}

// runAutoFunding runs self-funding or wait-for-funding for the auto-mode chain.
// It is called once after the manager has started and clients are available.
func runAutoFunding(
	ctx context.Context,
	mgr *chain.Manager,
	rawClients map[string]*tx.Client,
	cfg *config.ChainConfig,
	fundMnemonic string,
) error {
	if len(cfg.FeeTokens) == 0 {
		return nil
	}
	denom := cfg.FeeTokens[0].Denom
	pourClient, ok := rawClients[cfg.ChainID]
	if !ok {
		return fmt.Errorf("auto-fund: no client for chain %s", cfg.ChainID)
	}
	pourAddr, err := pourClient.AddressForKey(0)
	if err != nil {
		return fmt.Errorf("auto-fund: derive pour address: %w", err)
	}

	if fundMnemonic != "" {
		snap, ok := mgr.GetActive(cfg.ChainID)
		if !ok {
			return fmt.Errorf("auto-fund: chain %s not active", cfg.ChainID)
		}
		return devnet.SelfFund(ctx, snap.Info, fundMnemonic, pourAddr, denom)
	}
	return devnet.WaitForFunding(ctx, pourClient, pourAddr, denom)
}

// lowGasPriceFn builds a gascache.LowGasPriceFn from the loaded config.
func lowGasPriceFn(cfg *config.ChainsConfig) (gascache.LowGasPriceFn, error) {
	m := make(map[string]string, len(cfg.Chains))
	for i := range cfg.Chains {
		ch := &cfg.Chains[i]
		info, err := ch.ToChainInfo()
		if err != nil {
			return nil, err
		}
		if len(info.FeeTokens) > 0 && !info.FeeTokens[0].LowGasPrice.IsZero() {
			m[ch.ChainID] = info.FeeTokens[0].LowGasPrice.String()
		}
	}
	return func(chainID string) (string, bool) {
		v, ok := m[chainID]
		return v, ok && v != ""
	}, nil
}
