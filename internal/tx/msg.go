package tx

import (
	"fmt"
	"strconv"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	bankv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/bank/v1beta1"
	basev1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/v1beta1"
	transferv1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/ibc/applications/transfer/v1"
)

// buildMsgSend encodes a MsgSend as a proto Any ready for inclusion in TxBody.
func buildMsgSend(from, to string, coins Coins) (*anypb.Any, error) {
	pbCoins := make([]*basev1beta1.Coin, len(coins))
	for i, c := range coins {
		pbCoins[i] = &basev1beta1.Coin{Denom: c.Denom, Amount: c.Amount}
	}
	msg := &bankv1beta1.MsgSend{
		FromAddress: from,
		ToAddress:   to,
		Amount:      pbCoins,
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal MsgSend: %w", err)
	}
	return &anypb.Any{
		TypeUrl: "/cosmos.bank.v1beta1.MsgSend",
		Value:   b,
	}, nil
}

// buildMsgMultiSend encodes a MsgMultiSend as a proto Any ready for inclusion in TxBody.
// One input (from, sum of all output coins) and N outputs. Returns an error if outputs
// is empty or if coin amounts cannot be summed.
func buildMsgMultiSend(from string, outputs []SendOutput) (*anypb.Any, error) {
	if len(outputs) == 0 {
		return nil, fmt.Errorf("tx: buildMsgMultiSend: outputs must not be empty")
	}

	// Sum all output coins per denom for the single input.
	denomTotals := make(map[string]int64)
	denomOrder := make([]string, 0)
	for _, out := range outputs {
		for _, c := range out.Coins {
			if _, ok := denomTotals[c.Denom]; !ok {
				denomOrder = append(denomOrder, c.Denom)
			}
			n, err := parseCoinAmount(c.Amount)
			if err != nil {
				return nil, fmt.Errorf("tx: buildMsgMultiSend: output coin %q: %w", c.Denom, err)
			}
			denomTotals[c.Denom] += n
		}
	}

	inputCoins := make([]*basev1beta1.Coin, len(denomOrder))
	for i, denom := range denomOrder {
		inputCoins[i] = &basev1beta1.Coin{
			Denom:  denom,
			Amount: fmt.Sprintf("%d", denomTotals[denom]),
		}
	}

	pbOutputs := make([]*bankv1beta1.Output, len(outputs))
	for i, out := range outputs {
		pbCoins := make([]*basev1beta1.Coin, len(out.Coins))
		for j, c := range out.Coins {
			pbCoins[j] = &basev1beta1.Coin{Denom: c.Denom, Amount: c.Amount}
		}
		pbOutputs[i] = &bankv1beta1.Output{
			Address: out.ToAddress,
			Coins:   pbCoins,
		}
	}

	msg := &bankv1beta1.MsgMultiSend{
		Inputs: []*bankv1beta1.Input{{
			Address: from,
			Coins:   inputCoins,
		}},
		Outputs: pbOutputs,
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal MsgMultiSend: %w", err)
	}
	return &anypb.Any{
		TypeUrl: "/cosmos.bank.v1beta1.MsgMultiSend",
		Value:   b,
	}, nil
}

// buildMsgTransfer encodes a MsgTransfer as a proto Any ready for inclusion in TxBody.
// timeout_height is always zero — we use timeout_timestamp only (IBC v2 requirement).
func buildMsgTransfer(
	senderAddress, receiverAddress string,
	sourcePort, sourceChannel string,
	token Coin,
	timeoutTimestamp uint64,
	memo string,
) (*anypb.Any, error) {
	msg := &transferv1.MsgTransfer{
		SourcePort:       sourcePort,
		SourceChannel:    sourceChannel,
		Token:            &basev1beta1.Coin{Denom: token.Denom, Amount: token.Amount},
		Sender:           senderAddress,
		Receiver:         receiverAddress,
		TimeoutTimestamp: timeoutTimestamp,
		Memo:             memo,
		// TimeoutHeight intentionally omitted (nil) — timeout_timestamp is sufficient.
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal MsgTransfer: %w", err)
	}
	return &anypb.Any{
		TypeUrl: "/ibc.applications.transfer.v1.MsgTransfer",
		Value:   b,
	}, nil
}

// parseCoinAmount parses a decimal integer string into int64.
func parseCoinAmount(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", s, err)
	}
	return n, nil
}
