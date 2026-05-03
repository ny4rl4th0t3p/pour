package tx

import (
	"errors"
	"fmt"

	abciv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/abci/v1beta1"
)

var (
	ErrAccountNotFound    = errors.New("tx: account not found")
	ErrSequenceMismatch   = errors.New("tx: sequence mismatch")
	ErrInsufficientFee    = errors.New("tx: insufficient fee")
	ErrInsufficientGas    = errors.New("tx: insufficient gas")
	ErrInsufficientFunds  = errors.New("tx: insufficient funds")
	ErrChainUnreachable   = errors.New("tx: chain unreachable")
	ErrSimulationFailed   = errors.New("tx: simulation failed")
	ErrBroadcastTimeout   = errors.New("tx: broadcast timeout")
	ErrUnknownAccountType = errors.New("tx: unknown account type")
)

// ABCI error codes from the Cosmos SDK.
const (
	abciCodeInsufficientFunds = 5
	abciCodeOutOfGas          = 11
	abciCodeInsufficientFee   = 13
	abciCodeWrongSequence     = 32
)

// classifyChainError maps an ABCI response code to a sentinel error.
func classifyChainError(resp *abciv1beta1.TxResponse) error {
	switch resp.Code {
	case abciCodeInsufficientFunds:
		return ErrInsufficientFunds
	case abciCodeOutOfGas:
		return ErrInsufficientGas
	case abciCodeInsufficientFee:
		return ErrInsufficientFee
	case abciCodeWrongSequence:
		return ErrSequenceMismatch
	default:
		return fmt.Errorf("tx: chain error code %d: %s", resp.Code, resp.RawLog)
	}
}
