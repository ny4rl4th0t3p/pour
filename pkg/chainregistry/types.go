package chainregistry

import (
	"time"

	"github.com/shopspring/decimal"
)

// NetworkType classifies a chain as mainnet, testnet, or devnet.
type NetworkType string

const (
	NetworkTypeMainnet NetworkType = "mainnet"
	NetworkTypeTestnet NetworkType = "testnet"
	NetworkTypeDevnet  NetworkType = "devnet"
)

// KeyAlgo identifies the signing algorithm used by a chain.
type KeyAlgo string

const (
	KeyAlgoSecp256k1    KeyAlgo = "secp256k1"
	KeyAlgoEthsecp256k1 KeyAlgo = "ethsecp256k1"
)

// Source identifies which data layer a resolved field came from.
type Source int

const (
	SourceLive   Source = iota // live registry fetch
	SourceConfig               // chains.yml operator config
)

// Endpoint is a single network endpoint with an optional provider label.
type Endpoint struct {
	URL      string
	Provider string
}

// Endpoints groups protocol-typed endpoint lists for a chain.
type Endpoints struct {
	GRPC []Endpoint
	RPC  []Endpoint
	REST []Endpoint
}

// FeeToken describes a single accepted fee token and its gas price tiers.
// Gas prices use decimal.Decimal to avoid float64 representation errors.
type FeeToken struct {
	Denom           string
	LowGasPrice     decimal.Decimal
	AverageGasPrice decimal.Decimal
	HighGasPrice    decimal.Decimal
	Display         string
	Exponent        uint32
}

// FieldSources tracks which data layer each field group was last written from.
// It is a parallel struct to ChainInfo — one Source value per logical group —
// used by "pour chains diff" and GET /admin/registry/snapshot to show provenance.
type FieldSources struct {
	Identity  Source // ChainID, ChainName, NetworkType, PrettyName
	Address   Source // Bech32Prefix, Slip44, KeyAlgo
	Endpoints Source
	FeeTokens Source
	BlockTime Source
}

func (s *FieldSources) setAll(src Source) {
	s.Identity = src
	s.Address = src
	s.Endpoints = src
	s.FeeTokens = src
	s.BlockTime = src
}

// IBCChannel describes a single ICS20 transfer channel between two chains,
// as read from the cosmos/chain-registry _IBC/ directory.
type IBCChannel struct {
	// Chain names (registry chain_name, not chain_id) on each side.
	ChainNameA string
	ChainNameB string

	// Channel IDs and port IDs on each side.
	ChannelA string // e.g. "channel-0"
	ChannelB string // e.g. "channel-141"
	PortA    string // almost always "transfer"
	PortB    string // almost always "transfer"

	// Metadata from the registry.
	Ordering  string // "unordered"
	Version   string // "ics20-1"
	Status    string // "live" | "upcoming" | "killed"
	Preferred bool
}

// ChannelFor returns the channel ID, port, and peer chain name on the given
// chain's side of this pair. Returns ("", "", "", false) if chainName is not
// one of the two sides.
func (c IBCChannel) ChannelFor(chainName string) (channelID, port, peer string, ok bool) {
	switch chainName {
	case c.ChainNameA:
		return c.ChannelA, c.PortA, c.ChainNameB, true
	case c.ChainNameB:
		return c.ChannelB, c.PortB, c.ChainNameA, true
	default:
		return "", "", "", false
	}
}

// ChainInfo is the canonical runtime representation of a chain's configuration.
// It is the type all consumers import and read. Values are immutable once
// published: Store.UpdateLive and Store.Accept allocate a new ChainInfo and
// swap the pointer; the struct behind a pointer is never mutated in place.
type ChainInfo struct {
	// Identity
	ChainID     string
	ChainName   string
	NetworkType NetworkType
	PrettyName  string

	// Address derivation
	Bech32Prefix string
	Slip44       uint32
	KeyAlgo      KeyAlgo

	// Endpoints
	Endpoints Endpoints

	// Fee tokens
	FeeTokens []FeeToken

	// Operational
	BlockTime   time.Duration
	Enabled     bool      // set from operator config; not part of the registry schema
	LastChanged time.Time // when any resolved field last changed; zero for never-updated chains

	// Source provenance per field group — for diff display and audit.
	// Not classified by the field policy; managed internally by the resolver.
	Sources FieldSources
}
