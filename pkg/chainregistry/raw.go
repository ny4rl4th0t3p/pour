package chainregistry

import (
	"encoding/json"
	"strings"

	"github.com/shopspring/decimal"
)

// rawChainInfo is the on-disk representation used in registry.json snapshots.
// It mirrors the pruned cosmos/chain-registry chain.json schema. Unexported so
// the on-disk format can evolve without breaking external consumers of ChainInfo.
type rawChainInfo struct {
	ChainID      string   `json:"chain_id"`
	ChainName    string   `json:"chain_name"`
	NetworkType  string   `json:"network_type"`
	PrettyName   string   `json:"pretty_name"`
	Bech32Prefix string   `json:"bech32_prefix"`
	Slip44       uint32   `json:"slip44"`
	KeyAlgos     []string `json:"key_algos"`
	Fees         rawFees  `json:"fees"`
	APIs         rawAPIs  `json:"apis"`
}

type rawFees struct {
	FeeTokens []rawFeeToken `json:"fee_tokens"`
}

type rawFeeToken struct {
	Denom            string      `json:"denom"`
	FixedMinGasPrice json.Number `json:"fixed_min_gas_price"`
	LowGasPrice      json.Number `json:"low_gas_price"`
	AverageGasPrice  json.Number `json:"average_gas_price"`
	HighGasPrice     json.Number `json:"high_gas_price"`
	Display          string      `json:"display"`
	Exponent         uint32      `json:"exponent"`
}

type rawAPIs struct {
	GRPC []rawEndpoint `json:"grpc"`
	RPC  []rawEndpoint `json:"rpc"`
	REST []rawEndpoint `json:"rest"`
}

type rawEndpoint struct {
	Address  string `json:"address"`
	Provider string `json:"provider"`
}

// Snapshot holds parsed chain data fetched from the live registry.
// The Chains field uses the unexported rawChainInfo type so external callers
// cannot inspect chain data directly — they receive resolved *ChainInfo via Get.
type Snapshot struct {
	Revision    string
	Chains      map[string]rawChainInfo
	IBCChannels []IBCChannel
}

// rawSnapshot is the top-level format of registry.json.
type rawSnapshot struct {
	Chains map[string]rawChainInfo `json:"chains"`
}

// rawToInfo converts an unexported rawChainInfo into a public ChainInfo.
func rawToInfo(raw rawChainInfo) ChainInfo {
	info := ChainInfo{
		ChainID:      raw.ChainID,
		ChainName:    raw.ChainName,
		NetworkType:  networkTypeFromString(raw.NetworkType),
		PrettyName:   raw.PrettyName,
		Bech32Prefix: raw.Bech32Prefix,
		Slip44:       raw.Slip44,
		KeyAlgo:      keyAlgoFromStrings(raw.KeyAlgos),
		Endpoints: Endpoints{
			GRPC: rawEndpointsToEndpoints(raw.APIs.GRPC),
			RPC:  rawEndpointsToEndpoints(raw.APIs.RPC),
			REST: rawEndpointsToEndpoints(raw.APIs.REST),
		},
	}
	for _, rt := range raw.Fees.FeeTokens {
		info.FeeTokens = append(info.FeeTokens, FeeToken{
			Denom:           rt.Denom,
			LowGasPrice:     decimalFromNumber(rt.LowGasPrice),
			AverageGasPrice: decimalFromNumber(rt.AverageGasPrice),
			HighGasPrice:    decimalFromNumber(rt.HighGasPrice),
			Display:         rt.Display,
			Exponent:        rt.Exponent,
		})
	}
	return info
}

func networkTypeFromString(s string) NetworkType {
	switch strings.ToLower(s) {
	case "testnet":
		return NetworkTypeTestnet
	case "devnet":
		return NetworkTypeDevnet
	default:
		return NetworkTypeMainnet
	}
}

func keyAlgoFromStrings(algos []string) KeyAlgo {
	for _, a := range algos {
		if strings.EqualFold(a, "ethsecp256k1") {
			return KeyAlgoEthsecp256k1
		}
	}
	return KeyAlgoSecp256k1
}

func rawEndpointsToEndpoints(raw []rawEndpoint) []Endpoint {
	out := make([]Endpoint, 0, len(raw))
	for _, r := range raw {
		out = append(out, Endpoint{URL: r.Address, Provider: r.Provider})
	}
	return out
}

// decimalFromNumber parses a json.Number into a decimal.Decimal.
// An empty or unparseable value returns zero.
func decimalFromNumber(n json.Number) decimal.Decimal {
	if n == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(n.String())
	if err != nil {
		return decimal.Zero
	}
	return d
}
