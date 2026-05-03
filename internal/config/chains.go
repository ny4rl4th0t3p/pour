package config

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/ny4rl4th0t3p/pour/internal/tx"
)

// ChainsConfig is the top-level structure of chains.yml.
type ChainsConfig struct {
	Abuse  AbuseConfig   `koanf:"abuse"`
	Chains []ChainConfig `koanf:"chains"`
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

// ChainConfig is the per-chain operator configuration.
type ChainConfig struct {
	ChainID      string           `koanf:"chain_id"`
	Enabled      bool             `koanf:"enabled"`
	Bech32Prefix string           `koanf:"bech32_prefix"`
	Slip44       uint32           `koanf:"slip44"`
	Endpoints    EndpointsConfig  `koanf:"endpoints"`
	FeeTokens    []FeeTokenConfig `koanf:"fee_tokens"`
	Drip         DripConfig       `koanf:"drip"`
}

// EndpointsConfig holds endpoint overrides for a chain.
type EndpointsConfig struct {
	GRPC []string `koanf:"grpc"`
}

// FeeTokenConfig describes an accepted fee token for a chain.
type FeeTokenConfig struct {
	Denom           string `koanf:"denom"`
	AverageGasPrice string `koanf:"average_gas_price"`
	LowGasPrice     string `koanf:"low_gas_price"`
}

// DripConfig holds the drip amounts for a chain.
// Anonymous and MaxPerAddressPerDay are required for enabled chains.
type DripConfig struct {
	Anonymous           string `koanf:"anonymous"`
	Signed              string `koanf:"signed"`
	MaxPerAddressPerDay string `koanf:"max_per_address_per_day"`
	Memo                string `koanf:"memo"`
}

// LoadChains parses the chains.yml file at path and validates required fields
// for every enabled chain.
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

	for i := range cfg.Chains {
		chain := &cfg.Chains[i]
		if !chain.Enabled {
			continue
		}
		if chain.Drip.Anonymous == "" {
			return nil, fmt.Errorf("config: chain %q: drip.anonymous is required", chain.ChainID)
		}
		if _, err := ParseCoin(chain.Drip.Anonymous); err != nil {
			return nil, fmt.Errorf("config: chain %q: drip.anonymous: %w", chain.ChainID, err)
		}
		if chain.Drip.MaxPerAddressPerDay == "" {
			return nil, fmt.Errorf("config: chain %q: drip.max_per_address_per_day is required", chain.ChainID)
		}
		if _, err := ParseCoin(chain.Drip.MaxPerAddressPerDay); err != nil {
			return nil, fmt.Errorf("config: chain %q: drip.max_per_address_per_day: %w", chain.ChainID, err)
		}
	}

	return &cfg, nil
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
