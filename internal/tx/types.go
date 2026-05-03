package tx

import "context"

// Coin is a (denom, amount) pair. Amount is a decimal integer string, e.g. "1000000".
type Coin struct {
	Denom  string
	Amount string
}

// Coins is an ordered list of Coin.
type Coins []Coin

// FeeToken describes one of a chain's accepted fee tokens.
type FeeToken struct {
	Denom           string
	AverageGasPrice string // decimal, e.g. "0.025"
	LowGasPrice     string // floor used by gascache decay
}

// ChainConfig is the minimal chain configuration needed by the tx client.
// GRPCEndpoint is host:port; port 443 implies TLS, otherwise insecure.
type ChainConfig struct {
	ChainID      string
	GRPCEndpoint string
	Bech32Prefix string
	Slip44       uint32
	FeeTokens    []FeeToken
}

// CachedEstimate holds empirically learned gas parameters for a chain.
type CachedEstimate struct {
	BaseGas        uint64
	GasPerOutput   uint64
	FeeDenom       string
	GasPriceAmount string // decimal
	SampleCount    int
}

// IsTrusted returns true when there are enough samples to use the tighter gas adjustment.
func (c *CachedEstimate) IsTrusted() bool { return c.SampleCount >= 5 }

// GasCache is the read interface for empirical gas estimates.
// Implemented by internal/gascache in M5; pass nil to skip cache lookup.
type GasCache interface {
	Lookup(ctx context.Context, chainID string) (*CachedEstimate, bool)
}

// SendRequest is the input to Client.BuildAndBroadcast.
type SendRequest struct {
	Mnemonic  string
	KeyIndex  uint32
	ToAddress string
	Coins     Coins
	GasCache  GasCache // optional
}

// BroadcastResult is the output of Client.BuildAndBroadcast.
type BroadcastResult struct {
	TxHash string
	Height int64
}

// Account holds the on-chain account state needed for signing.
type Account struct {
	Address       string
	AccountNumber uint64
	Sequence      uint64
}

// Estimate holds the output of fee estimation.
type Estimate struct {
	GasLimit uint64
	Fee      Coin
}
