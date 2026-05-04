package tx

import (
	"errors"
	"testing"

	abciv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/abci/v1beta1"
)

func TestClassifyChainError_insufficientFunds(t *testing.T) {
	err := classifyChainError(&abciv1beta1.TxResponse{Code: abciCodeInsufficientFunds})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("code %d: got %v, want ErrInsufficientFunds", abciCodeInsufficientFunds, err)
	}
}

func TestClassifyChainError_outOfGas(t *testing.T) {
	err := classifyChainError(&abciv1beta1.TxResponse{Code: abciCodeOutOfGas})
	if !errors.Is(err, ErrInsufficientGas) {
		t.Errorf("code %d: got %v, want ErrInsufficientGas", abciCodeOutOfGas, err)
	}
}

func TestClassifyChainError_insufficientFee(t *testing.T) {
	err := classifyChainError(&abciv1beta1.TxResponse{Code: abciCodeInsufficientFee})
	if !errors.Is(err, ErrInsufficientFee) {
		t.Errorf("code %d: got %v, want ErrInsufficientFee", abciCodeInsufficientFee, err)
	}
}

func TestClassifyChainError_wrongSequence(t *testing.T) {
	err := classifyChainError(&abciv1beta1.TxResponse{Code: abciCodeWrongSequence})
	if !errors.Is(err, ErrSequenceMismatch) {
		t.Errorf("code %d: got %v, want ErrSequenceMismatch", abciCodeWrongSequence, err)
	}
}

func TestClassifyChainError_unknownCode(t *testing.T) {
	resp := &abciv1beta1.TxResponse{Code: 99, RawLog: "something went wrong"}
	err := classifyChainError(resp)
	if err == nil {
		t.Fatal("expected non-nil error for unknown code")
	}
	if errors.Is(err, ErrInsufficientFunds) || errors.Is(err, ErrInsufficientGas) ||
		errors.Is(err, ErrInsufficientFee) || errors.Is(err, ErrSequenceMismatch) {
		t.Errorf("unexpected sentinel error for unknown code 99: %v", err)
	}
}
