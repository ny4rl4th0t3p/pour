package tx

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	bankv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/bank/v1beta1"
	basev1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/v1beta1"
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
