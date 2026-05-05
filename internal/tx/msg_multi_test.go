package tx

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	bankv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/bank/v1beta1"
)

func TestBuildMsgMultiSend_emptyOutputs(t *testing.T) {
	_, err := buildMsgMultiSend(testFromAddr, nil)
	if err == nil {
		t.Fatal("expected error for empty outputs")
	}
}

func TestBuildMsgMultiSend_homogeneousCoins(t *testing.T) {
	outputs := []SendOutput{
		{ToAddress: testToAddr, Coins: Coins{{Denom: "uosmo", Amount: "1000000"}}},
		{ToAddress: testFromAddr, Coins: Coins{{Denom: "uosmo", Amount: "2000000"}}},
	}

	msgAny, err := buildMsgMultiSend(testFromAddr, outputs)
	if err != nil {
		t.Fatalf("buildMsgMultiSend: %v", err)
	}

	var msg bankv1beta1.MsgMultiSend
	if err := proto.Unmarshal(msgAny.Value, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(msg.Inputs) != 1 {
		t.Fatalf("inputs: got %d, want 1", len(msg.Inputs))
	}
	if msg.Inputs[0].Address != testFromAddr {
		t.Errorf("input address: got %s, want %s", msg.Inputs[0].Address, testFromAddr)
	}
	if len(msg.Inputs[0].Coins) != 1 {
		t.Fatalf("input coins: got %d denoms, want 1", len(msg.Inputs[0].Coins))
	}
	if msg.Inputs[0].Coins[0].Amount != "3000000" {
		t.Errorf("input total: got %s, want 3000000", msg.Inputs[0].Coins[0].Amount)
	}

	if len(msg.Outputs) != 2 {
		t.Fatalf("outputs: got %d, want 2", len(msg.Outputs))
	}
}

func TestBuildMsgMultiSend_mixedDenoms(t *testing.T) {
	outputs := []SendOutput{
		{ToAddress: testToAddr, Coins: Coins{{Denom: "uosmo", Amount: "1000000"}, {Denom: "uatom", Amount: "500000"}}},
		{ToAddress: testFromAddr, Coins: Coins{{Denom: "uosmo", Amount: "2000000"}}},
	}

	msgAny, err := buildMsgMultiSend(testFromAddr, outputs)
	if err != nil {
		t.Fatalf("buildMsgMultiSend: %v", err)
	}

	var msg bankv1beta1.MsgMultiSend
	if err := proto.Unmarshal(msgAny.Value, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Input must cover both denoms.
	inputDenoms := make(map[string]string)
	for _, c := range msg.Inputs[0].Coins {
		inputDenoms[c.Denom] = c.Amount
	}
	if inputDenoms["uosmo"] != "3000000" {
		t.Errorf("input uosmo: got %s, want 3000000", inputDenoms["uosmo"])
	}
	if inputDenoms["uatom"] != "500000" {
		t.Errorf("input uatom: got %s, want 500000", inputDenoms["uatom"])
	}

	if len(msg.Outputs) != 2 {
		t.Fatalf("outputs: got %d, want 2", len(msg.Outputs))
	}
}

func TestBuildMsgMultiSend_invalidAmount(t *testing.T) {
	outputs := []SendOutput{
		{ToAddress: testToAddr, Coins: Coins{{Denom: "uosmo", Amount: "not-a-number"}}},
	}
	_, err := buildMsgMultiSend(testFromAddr, outputs)
	if err == nil {
		t.Fatal("expected error for invalid coin amount")
	}
}

func TestIsSequenceMismatch_sentinelError(t *testing.T) {
	if !IsSequenceMismatch(ErrSequenceMismatch) {
		t.Error("expected true for ErrSequenceMismatch sentinel")
	}
	if !IsSequenceMismatch(fmt.Errorf("wrap: %w", ErrSequenceMismatch)) {
		t.Error("expected true for wrapped ErrSequenceMismatch")
	}
}

func TestIsSequenceMismatch_gRPCString(t *testing.T) {
	grpcErr := errors.New("rpc error: code = Unknown desc = account sequence mismatch, expected 5, got 4")
	if !IsSequenceMismatch(grpcErr) {
		t.Error("expected true for gRPC sequence mismatch string")
	}
}

func TestIsSequenceMismatch_unrelated(t *testing.T) {
	if IsSequenceMismatch(ErrInsufficientFunds) {
		t.Error("expected false for ErrInsufficientFunds")
	}
	if IsSequenceMismatch(nil) {
		t.Error("expected false for nil")
	}
}

func TestIsInsufficientFee(t *testing.T) {
	if !IsInsufficientFee(ErrInsufficientFee) {
		t.Error("expected true for ErrInsufficientFee sentinel")
	}
	if !IsInsufficientFee(fmt.Errorf("wrap: %w", ErrInsufficientFee)) {
		t.Error("expected true for wrapped ErrInsufficientFee")
	}
	if IsInsufficientFee(ErrInsufficientFunds) {
		t.Error("expected false for ErrInsufficientFunds")
	}
}
