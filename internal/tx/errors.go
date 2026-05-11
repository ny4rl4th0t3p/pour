package tx

import (
	"errors"
	"fmt"
	"strings"

	abciv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/abci/v1beta1"
)

var (
	ErrAccountNotFound     = errors.New("tx: account not found")
	ErrSequenceMismatch    = errors.New("tx: sequence mismatch")
	ErrInsufficientFee     = errors.New("tx: insufficient fee")
	ErrInsufficientGas     = errors.New("tx: insufficient gas")
	ErrInsufficientFunds   = errors.New("tx: insufficient funds")
	ErrChainUnreachable    = errors.New("tx: chain unreachable")
	ErrBroadcastTimeout    = errors.New("tx: broadcast timeout")
	ErrUnknownAccountType  = errors.New("tx: unknown account type")
	ErrNoEndpointAvailable = errors.New("tx: no endpoint available")

	errRESTUnavailable = errors.New("tx: REST endpoint unavailable")
)

// ABCI error codes from the Cosmos SDK.
const (
	abciCodeInsufficientFunds = 5
	abciCodeOutOfGas          = 11
	abciCodeInsufficientFee   = 13
	abciCodeWrongSequence     = 32
)

// IsSequenceMismatch returns true when err represents a sequence number conflict.
// Covers both the ABCI code 32 path and gRPC status strings from chains that surface
// the error before broadcast acceptance.
func IsSequenceMismatch(err error) bool {
	if errors.Is(err, ErrSequenceMismatch) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "account sequence mismatch")
}

// IsInsufficientFee returns true when err indicates the submitted fee was too low.
func IsInsufficientFee(err error) bool {
	return errors.Is(err, ErrInsufficientFee)
}

// classifyCodeAndLog maps an ABCI response code and raw log to a sentinel error.
// Used by both gRPC and REST paths.
func classifyCodeAndLog(code uint32, rawLog string) error {
	switch code {
	case abciCodeInsufficientFunds:
		return ErrInsufficientFunds
	case abciCodeOutOfGas:
		return ErrInsufficientGas
	case abciCodeInsufficientFee:
		return ErrInsufficientFee
	case abciCodeWrongSequence:
		return fmt.Errorf("%w: %s", ErrSequenceMismatch, rawLog)
	default:
		return fmt.Errorf("tx: chain error code %d: %s", code, rawLog)
	}
}

// classifyChainError maps an ABCI proto response to a sentinel error.
func classifyChainError(resp *abciv1beta1.TxResponse) error {
	return classifyCodeAndLog(resp.Code, resp.RawLog)
}
