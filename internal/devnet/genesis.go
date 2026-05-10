package devnet

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const defaultBech32Prefix = "cosmos"

// GenesisInfo contains the fields extracted from a chain's genesis.json that
// are needed to auto-configure pour in --auto mode.
type GenesisInfo struct {
	ChainID      string
	Bech32Prefix string            // inferred from first bank balance address
	NativeDenom  string            // app_state.staking.params.bond_denom
	Balances     map[string][]Coin // address → coins
}

// Coin is a simple denom+amount pair extracted from genesis balances.
type Coin struct {
	Denom  string
	Amount string
}

// ParseGenesis reads <homePath>/config/genesis.json and returns the fields
// needed for auto-configure mode. Only the minimal subset of the genesis JSON
// is decoded; large module states (wasm, etc.) are skipped entirely.
func ParseGenesis(homePath string) (*GenesisInfo, error) {
	path := filepath.Join(homePath, "config", "genesis.json")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("devnet: open genesis at %s: %w", path, err)
	}
	defer f.Close()

	var raw struct {
		ChainID  string `json:"chain_id"`
		AppState struct {
			Staking struct {
				Params struct {
					BondDenom string `json:"bond_denom"`
				} `json:"params"`
			} `json:"staking"`
			Bank struct {
				Balances []struct {
					Address string `json:"address"`
					Coins   []struct {
						Denom  string `json:"denom"`
						Amount string `json:"amount"`
					} `json:"coins"`
				} `json:"balances"`
			} `json:"bank"`
		} `json:"app_state"`
	}

	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("devnet: parse genesis: %w", err)
	}

	if raw.ChainID == "" {
		return nil, fmt.Errorf("devnet: genesis missing chain_id")
	}

	info := &GenesisInfo{
		ChainID:     raw.ChainID,
		NativeDenom: raw.AppState.Staking.Params.BondDenom,
		Balances:    make(map[string][]Coin, len(raw.AppState.Bank.Balances)),
	}

	for _, b := range raw.AppState.Bank.Balances {
		coins := make([]Coin, len(b.Coins))
		for i, c := range b.Coins {
			coins[i] = Coin{Denom: c.Denom, Amount: c.Amount}
		}
		info.Balances[b.Address] = coins

		if info.Bech32Prefix == "" {
			info.Bech32Prefix = extractBech32Prefix(b.Address)
		}
	}

	if info.Bech32Prefix == "" {
		slog.Warn("devnet: could not infer bech32 prefix from genesis balances, defaulting to 'cosmos'")
		info.Bech32Prefix = defaultBech32Prefix
	}

	return info, nil
}

// extractBech32Prefix returns the human-readable part of a bech32 address.
// In bech32, the separator is the last '1' character; everything before it is
// the HRP. Only the prefix is needed here — full checksum decoding is not required.
func extractBech32Prefix(addr string) string {
	lower := strings.ToLower(strings.TrimSpace(addr))
	sep := strings.LastIndex(lower, "1")
	if sep < 1 {
		return ""
	}
	return lower[:sep]
}
