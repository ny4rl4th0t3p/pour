package config

import (
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/shopspring/decimal"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

// ChainsConfig is the top-level structure of chains.yml.
type ChainsConfig struct {
	Abuse    AbuseConfig    `koanf:"abuse"`
	Admin    AdminConfig    `koanf:"admin"`
	Registry RegistryConfig `koanf:"registry"`
	Chains   []ChainConfig  `koanf:"chains"`
}

// AbuseConfig holds abuse-prevention settings.
type AbuseConfig struct {
	IPRateLimit IPRateLimitConfig `koanf:"ip_rate_limit"`
}

// IPRateLimitConfig holds per-IP rate limit settings.
// Window is a Go duration string (e.g. "1h", "5m"); defaults to "1h" if unset.
type IPRateLimitConfig struct {
	RequestsPerWindow int    `koanf:"requests_per_window"`
	Window            string `koanf:"window"`
}

// WindowDuration parses Window and returns the configured duration.
// Returns time.Hour when Window is empty (default).
func (c IPRateLimitConfig) WindowDuration() (time.Duration, error) {
	if c.Window == "" {
		return time.Hour, nil
	}
	d, err := time.ParseDuration(c.Window)
	if err != nil {
		return 0, fmt.Errorf("config: abuse.ip_rate_limit.window %q: %w", c.Window, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: abuse.ip_rate_limit.window must be positive, got %q", c.Window)
	}
	return d, nil
}

// AdminConfig configures the admin API.
type AdminConfig struct {
	// AllowedCIDRs restricts admin endpoint access by source IP.
	// Defaults to ["127.0.0.1/32"] when empty.
	AllowedCIDRs []string `koanf:"allowed_cidrs"`
}

// RegistryConfig configures the live registry fetch.
type RegistryConfig struct {
	// BaseURL is the registry root URL. Defaults to the cosmos/chain-registry GitHub URL.
	BaseURL string `koanf:"base_url"`

	// RefreshInterval is a Go duration string (e.g. "6h"). Defaults to "6h" when empty.
	RefreshInterval string `koanf:"refresh_interval"`
}

const defaultRefreshInterval = 6 * time.Hour

// RefreshDuration parses RefreshInterval, returning 6h when unset.
func (r RegistryConfig) RefreshDuration() (time.Duration, error) {
	if r.RefreshInterval == "" {
		return defaultRefreshInterval, nil
	}
	d, err := time.ParseDuration(r.RefreshInterval)
	if err != nil {
		return 0, fmt.Errorf("config: registry.refresh_interval %q: %w", r.RefreshInterval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: registry.refresh_interval must be positive, got %q", r.RefreshInterval)
	}
	return d, nil
}

// ChainConfig is the per-chain operator configuration.
//
// Registry chains (Standalone == false) are fetched from the public registry.
// Only ChainID and Drip are required; all other fields are optional overrides.
//
// Standalone chains (Standalone == true) are not on any public registry.
// Bech32Prefix, Slip44, at least one endpoints.grpc, and at least one fee_tokens
// entry are required.
type ChainConfig struct {
	ChainID    string `koanf:"chain_id"`
	Standalone bool   `koanf:"standalone"`

	// Pointer fields: nil means "inherit from registry" (ignored for standalone).
	Enabled      *bool            `koanf:"enabled"`
	ChainName    *string          `koanf:"chain_name"`
	NetworkType  *string          `koanf:"network_type"`
	KeyAlgo      *string          `koanf:"key_algo"`
	Bech32Prefix *string          `koanf:"bech32_prefix"`
	Slip44       *uint32          `koanf:"slip44"`
	Endpoints    *EndpointsConfig `koanf:"endpoints"`
	FeeTokens    []FeeTokenConfig `koanf:"fee_tokens"`
	BlockTime    *string          `koanf:"block_time"`

	// Drip is required for enabled chains.
	Drip DripConfig `koanf:"drip"`

	// Concurrency fields.
	// Distributors: number of signing accounts (indices 1..N). 0 = default (1).
	// BatchWindow: flush interval; "0" disables batching (synchronous mode). Default "5s".
	// MaxRecipientsPerBatch: cap per MsgMultiSend. 0 = default (100).
	// MaxQueueDepth: per-distributor queue cap. 0 = default (500).
	// RefillThreshold: minimum distributor balance before holder tops it up (coin string).
	//   Empty = computed at startup as drip.anonymous × Distributors × 10.
	// RefillInterval: how often to check distributor balances. Default "1m".
	Distributors          int    `koanf:"distributors"`
	BatchWindow           string `koanf:"batch_window"`
	MaxRecipientsPerBatch int    `koanf:"max_recipients_per_batch"`
	MaxQueueDepth         int    `koanf:"max_queue_depth"`
	RefillThreshold       string `koanf:"refill_threshold"`
	RefillInterval        string `koanf:"refill_interval"`
}

// IsEnabled reports whether the chain is explicitly enabled.
func (c *ChainConfig) IsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// DistributorCount returns the effective number of distributors, defaulting to 1 when 0.
func (c *ChainConfig) DistributorCount() int {
	if c.Distributors <= 0 {
		return 1
	}
	return c.Distributors
}

// BatchWindowDuration parses BatchWindow, returning 5s when empty and 0 when "0" (sync mode).
func (c *ChainConfig) BatchWindowDuration() (time.Duration, error) {
	if c.BatchWindow == "" {
		return 5 * time.Second, nil
	}
	d, err := time.ParseDuration(c.BatchWindow)
	if err != nil {
		return 0, fmt.Errorf("config: chain %q: batch_window %q: %w", c.ChainID, c.BatchWindow, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("config: chain %q: batch_window %q: must be non-negative", c.ChainID, c.BatchWindow)
	}
	return d, nil
}

const (
	// DefaultMaxRecipientsPerBatch keeps gas cost and tx size within conservative chain limits.
	DefaultMaxRecipientsPerBatch = 25
	// DefaultMaxQueueDepth is the per-distributor queue cap when not configured.
	DefaultMaxQueueDepth = 500
)

// MaxRecipientsPerBatchOrDefault returns MaxRecipientsPerBatch, defaulting to DefaultMaxRecipientsPerBatch when 0.
func (c *ChainConfig) MaxRecipientsPerBatchOrDefault() int {
	if c.MaxRecipientsPerBatch <= 0 {
		return DefaultMaxRecipientsPerBatch
	}
	return c.MaxRecipientsPerBatch
}

// RefillIntervalOrDefault parses RefillInterval, returning 1m when empty.
func (c *ChainConfig) RefillIntervalOrDefault() (time.Duration, error) {
	if c.RefillInterval == "" {
		return time.Minute, nil
	}
	d, err := time.ParseDuration(c.RefillInterval)
	if err != nil {
		return 0, fmt.Errorf("config: chain %q: refill_interval %q: %w", c.ChainID, c.RefillInterval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: chain %q: refill_interval must be positive", c.ChainID)
	}
	return d, nil
}

// MaxQueueDepthOrDefault returns MaxQueueDepth, defaulting to DefaultMaxQueueDepth when 0.
func (c *ChainConfig) MaxQueueDepthOrDefault() int {
	if c.MaxQueueDepth <= 0 {
		return DefaultMaxQueueDepth
	}
	return c.MaxQueueDepth
}

// EndpointsConfig holds endpoint overrides for a chain.
// A non-nil slice fully replaces the registry list for that protocol.
type EndpointsConfig struct {
	GRPC []string `koanf:"grpc"`
	RPC  []string `koanf:"rpc"`
	REST []string `koanf:"rest"`
}

// FeeTokenConfig describes an accepted fee token and its gas price tiers.
// nil pointer fields inherit the registry value (ignored for standalone chains).
type FeeTokenConfig struct {
	Denom           string  `koanf:"denom"`
	LowGasPrice     *string `koanf:"low_gas_price"`
	AverageGasPrice *string `koanf:"average_gas_price"`
	HighGasPrice    *string `koanf:"high_gas_price"`
}

// DripConfig holds the drip amounts for a chain.
// Anonymous and MaxPerAddressPerDay are required for enabled chains.
type DripConfig struct {
	Anonymous           string `koanf:"anonymous"`
	Signed              string `koanf:"signed"`
	MaxPerAddressPerDay string `koanf:"max_per_address_per_day"`
	Memo                string `koanf:"memo"`
}

// LoadChains parses the chains.yml file at path and validates required fields.
func LoadChains(path string) (*ChainsConfig, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load %s: %w", path, err)
	}

	var cfg ChainsConfig
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	if _, err := cfg.Abuse.IPRateLimit.WindowDuration(); err != nil {
		return nil, err
	}
	if _, err := cfg.Registry.RefreshDuration(); err != nil {
		return nil, err
	}
	for _, cidr := range cfg.Admin.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("config: admin.allowed_cidrs: invalid CIDR %q: %w", cidr, err)
		}
	}

	for i := range cfg.Chains {
		if err := validateChain(i, &cfg.Chains[i]); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

func validateChain(i int, chain *ChainConfig) error {
	if chain.ChainID == "" {
		return fmt.Errorf("config: chain at index %d: chain_id is required", i)
	}
	if chain.BlockTime != nil {
		d, err := time.ParseDuration(*chain.BlockTime)
		if err != nil {
			return fmt.Errorf("config: chain %q: block_time: %w", chain.ChainID, err)
		}
		if d <= 0 {
			return fmt.Errorf("config: chain %q: block_time %q: must be positive", chain.ChainID, *chain.BlockTime)
		}
	}
	if err := validateConcurrencyFields(chain); err != nil {
		return err
	}
	if chain.Standalone {
		if err := validateStandalone(chain); err != nil {
			return err
		}
	}
	if !chain.IsEnabled() {
		return nil
	}
	if chain.Drip.Anonymous == "" {
		return fmt.Errorf("config: chain %q: drip.anonymous is required", chain.ChainID)
	}
	if _, err := ParseCoin(chain.Drip.Anonymous); err != nil {
		return fmt.Errorf("config: chain %q: drip.anonymous: %w", chain.ChainID, err)
	}
	if chain.Drip.MaxPerAddressPerDay == "" {
		return fmt.Errorf("config: chain %q: drip.max_per_address_per_day is required", chain.ChainID)
	}
	if _, err := ParseCoin(chain.Drip.MaxPerAddressPerDay); err != nil {
		return fmt.Errorf("config: chain %q: drip.max_per_address_per_day: %w", chain.ChainID, err)
	}
	return nil
}

func validateStandalone(chain *ChainConfig) error {
	if chain.Bech32Prefix == nil || *chain.Bech32Prefix == "" {
		return fmt.Errorf("config: standalone chain %q: bech32_prefix is required", chain.ChainID)
	}
	if chain.Slip44 == nil {
		return fmt.Errorf("config: standalone chain %q: slip44 is required", chain.ChainID)
	}
	if chain.Endpoints == nil || len(chain.Endpoints.GRPC) == 0 {
		return fmt.Errorf("config: standalone chain %q: at least one endpoints.grpc is required", chain.ChainID)
	}
	if len(chain.FeeTokens) == 0 {
		return fmt.Errorf("config: standalone chain %q: at least one fee_tokens entry is required", chain.ChainID)
	}
	return nil
}

func validateConcurrencyFields(chain *ChainConfig) error {
	if chain.Distributors < 0 {
		return fmt.Errorf("config: chain %q: distributors must be >= 0", chain.ChainID)
	}
	if chain.BatchWindow != "" {
		if _, err := chain.BatchWindowDuration(); err != nil {
			return err
		}
	}
	if chain.MaxRecipientsPerBatch < 0 {
		return fmt.Errorf("config: chain %q: max_recipients_per_batch must be >= 0", chain.ChainID)
	}
	if chain.MaxQueueDepth < 0 {
		return fmt.Errorf("config: chain %q: max_queue_depth must be >= 0", chain.ChainID)
	}
	if chain.RefillThreshold != "" {
		if _, err := ParseCoin(chain.RefillThreshold); err != nil {
			return fmt.Errorf("config: chain %q: refill_threshold: %w", chain.ChainID, err)
		}
	}
	if chain.RefillInterval != "" {
		if _, err := chain.RefillIntervalOrDefault(); err != nil {
			return err
		}
	}
	return nil
}

// ToOverrideSet converts all registry chains (non-standalone) into a
// chainregistry.OverrideSet. Called at daemon startup and on config reload.
func (c *ChainsConfig) ToOverrideSet() (*chainregistry.OverrideSet, error) {
	ov := &chainregistry.OverrideSet{
		Chains: make(map[string]*chainregistry.ChainOverride, len(c.Chains)),
	}
	for i := range c.Chains {
		chain := &c.Chains[i]
		if chain.Standalone {
			continue
		}
		co := &chainregistry.ChainOverride{
			Enabled:      chain.Enabled,
			ChainName:    chain.ChainName,
			Bech32Prefix: chain.Bech32Prefix,
			Slip44:       chain.Slip44,
			Distributors: chain.Distributors,
		}
		if chain.NetworkType != nil {
			nt := chainregistry.NetworkType(*chain.NetworkType)
			co.NetworkType = &nt
		}
		if chain.KeyAlgo != nil {
			ka := chainregistry.KeyAlgo(*chain.KeyAlgo)
			co.KeyAlgo = &ka
		}
		if chain.BlockTime != nil {
			d, err := time.ParseDuration(*chain.BlockTime)
			if err != nil {
				return nil, fmt.Errorf("config: chain %q: block_time: %w", chain.ChainID, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("config: chain %q: block_time %q: must be positive", chain.ChainID, *chain.BlockTime)
			}
			co.BlockTime = &d
		}
		if chain.Endpoints != nil {
			co.Endpoints = &chainregistry.EndpointsOverride{
				GRPC: chain.Endpoints.GRPC,
				RPC:  chain.Endpoints.RPC,
				REST: chain.Endpoints.REST,
			}
		}
		for _, ft := range chain.FeeTokens {
			co.FeeTokens = append(co.FeeTokens, chainregistry.FeeTokenOverride{
				Denom:           ft.Denom,
				LowGasPrice:     ft.LowGasPrice,
				AverageGasPrice: ft.AverageGasPrice,
				HighGasPrice:    ft.HighGasPrice,
			})
		}
		ov.Chains[chain.ChainID] = co
	}
	return ov, nil
}

// ToChainInfo converts a ChainConfig to a *chainregistry.ChainInfo.
// Nil pointer fields produce zero values.
func (c *ChainConfig) ToChainInfo() (*chainregistry.ChainInfo, error) {
	info := &chainregistry.ChainInfo{
		ChainID:      c.ChainID,
		Bech32Prefix: derefString(c.Bech32Prefix),
		Slip44:       derefUint32(c.Slip44),
		Enabled:      c.IsEnabled(),
	}
	if c.ChainName != nil {
		info.ChainName = *c.ChainName
	}
	if c.NetworkType != nil {
		info.NetworkType = chainregistry.NetworkType(*c.NetworkType)
	}
	if c.KeyAlgo != nil {
		info.KeyAlgo = chainregistry.KeyAlgo(*c.KeyAlgo)
	}
	if c.BlockTime != nil {
		d, err := time.ParseDuration(*c.BlockTime)
		if err != nil {
			return nil, fmt.Errorf("config: chain %q: block_time: %w", c.ChainID, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("config: chain %q: block_time %q: must be positive", c.ChainID, *c.BlockTime)
		}
		info.BlockTime = d
	}
	if c.Endpoints != nil {
		info.Endpoints.GRPC = stringsToEndpoints(c.Endpoints.GRPC)
		info.Endpoints.RPC = stringsToEndpoints(c.Endpoints.RPC)
		info.Endpoints.REST = stringsToEndpoints(c.Endpoints.REST)
	}
	for _, ft := range c.FeeTokens {
		info.FeeTokens = append(info.FeeTokens, chainregistry.FeeToken{
			Denom:           ft.Denom,
			LowGasPrice:     parseDecimal(ft.LowGasPrice),
			AverageGasPrice: parseDecimal(ft.AverageGasPrice),
			HighGasPrice:    parseDecimal(ft.HighGasPrice),
		})
	}
	return info, nil
}

// EnabledRegistryChainIDs returns the chain IDs of all enabled, non-standalone chains.
// These are the chains to fetch from the public registry on startup.
func (c *ChainsConfig) EnabledRegistryChainIDs() []string {
	var ids []string
	for i := range c.Chains {
		chain := &c.Chains[i]
		if !chain.Standalone && chain.IsEnabled() {
			ids = append(ids, chain.ChainID)
		}
	}
	return ids
}

// ToStandaloneInfos converts all standalone chains into chainregistry.ChainInfo
// values ready for store.AddStandalone. Called at daemon startup.
func (c *ChainsConfig) ToStandaloneInfos() ([]chainregistry.ChainInfo, error) {
	var infos []chainregistry.ChainInfo
	for i := range c.Chains {
		chain := &c.Chains[i]
		if !chain.Standalone {
			continue
		}
		info, err := chain.ToChainInfo()
		if err != nil {
			return nil, err
		}
		infos = append(infos, *info)
	}
	return infos, nil
}

// ParseCoin parses a Cosmos SDK coin string of the form "<amount><denom>"
// (e.g. "1000000uosmo") into a tx.Coin.
func ParseCoin(s string) (tx.Coin, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
	if i <= 0 {
		return tx.Coin{}, fmt.Errorf("config: invalid coin %q: must be <amount><denom>", s)
	}
	denom := strings.TrimSpace(s[i:])
	if denom == "" {
		return tx.Coin{}, fmt.Errorf("config: invalid coin %q: missing denom", s)
	}
	return tx.Coin{Amount: s[:i], Denom: denom}, nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefUint32(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}

func stringsToEndpoints(ss []string) []chainregistry.Endpoint {
	out := make([]chainregistry.Endpoint, 0, len(ss))
	for _, s := range ss {
		out = append(out, chainregistry.Endpoint{URL: s})
	}
	return out
}

func parseDecimal(p *string) decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(*p)
	if err != nil {
		return decimal.Zero
	}
	return d
}
