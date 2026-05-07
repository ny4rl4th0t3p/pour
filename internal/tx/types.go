package tx

import "context"

// MsgType* constants are the gas cache discriminator values used to keep gas
// profiles for different message types separate.
const (
	MsgTypeSend      = "send"
	MsgTypeMultiSend = "multisend"
	MsgTypeTransfer  = "transfer"
)

// Coin is a (denom, amount) pair. Amount is a decimal integer string, e.g. "1000000".
type Coin struct {
	Denom  string
	Amount string
}

// Coins is an ordered list of Coin.
type Coins []Coin

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

// GasCache is the interface for empirical gas estimates — both reading and writing.
// Implemented by internal/gascache. Pass nil to Options.GasCache to disable caching.
type GasCache interface {
	Lookup(ctx context.Context, chainID, msgType string) (*CachedEstimate, bool)
	RecordSuccess(ctx context.Context, chainID, msgType string, gasUsed uint64, outputCount int, feeDenom, gasPriceAmount string) error
	RecordFailure(ctx context.Context, chainID, msgType, reason string) error
}

// SendRequest is the input to Client.BuildAndBroadcast.
type SendRequest struct {
	KeyIndex  uint32
	ToAddress string
	Coins     Coins
}

// SendOutput is one recipient in a MsgMultiSend batch.
type SendOutput struct {
	ToAddress string
	Coins     Coins
}

// BatchSendRequest is the input to Client.BuildAndBroadcastMulti.
type BatchSendRequest struct {
	KeyIndex uint32
	Outputs  []SendOutput
}

// BroadcastResult is the output of Client.BuildAndBroadcast / BuildAndBroadcastMulti.
type BroadcastResult struct {
	TxHash  string
	Height  int64
	GasUsed uint64
}

// TransferRequest is the input to Client.BuildAndBroadcastTransfer.
type TransferRequest struct {
	KeyIndex         uint32
	SourcePort       string // typically "transfer"
	SourceChannel    string // e.g. "channel-0"
	Token            Coin
	ReceiverAddress  string // address on the destination chain
	TimeoutTimestamp uint64 // nanoseconds since unix epoch; required
	Memo             string
}

// TransferResult is the output of Client.BuildAndBroadcastTransfer.
type TransferResult struct {
	TxHash         string
	Height         int64
	GasUsed        uint64
	PacketSequence uint64 // 0 if not parseable from tx events
}

// Account holds the on-chain account state needed for signing.
type Account struct {
	Address       string
	AccountNumber uint64
	Sequence      uint64
}

// Estimate holds the output of fee estimation.
type Estimate struct {
	GasLimit       uint64
	Fee            Coin
	GasPriceAmount string // raw gas price used to compute Fee, e.g. "0.025"
}
