package tx

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	ethermintv1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/ethermint/types/v1"
)

type accountDecoder func(value []byte) (*Account, error)

var knownAccountDecoders = map[string]accountDecoder{
	"/cosmos.auth.v1beta1.BaseAccount": decodeBaseAccount,
	"/ethermint.types.v1.EthAccount":   decodeEthAccount,
}

func queryAccountGRPC(ctx context.Context, client authv1beta1.QueryClient, addr string) (*Account, error) {
	resp, err := client.Account(ctx, &authv1beta1.QueryAccountRequest{Address: addr})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("tx: query account: %w", err)
	}

	accountAny := resp.GetAccount()
	if accountAny == nil {
		return nil, ErrAccountNotFound
	}

	if dec, ok := knownAccountDecoders[accountAny.TypeUrl]; ok {
		return dec(accountAny.Value)
	}

	// Generic fallback: attempt to unmarshal as BaseAccount.
	acc, err := decodeBaseAccount(accountAny.Value)
	if err != nil {
		return nil, ErrUnknownAccountType
	}
	return acc, nil
}

func decodeBaseAccount(value []byte) (*Account, error) {
	var ba authv1beta1.BaseAccount
	if err := proto.Unmarshal(value, &ba); err != nil {
		return nil, fmt.Errorf("tx: decode BaseAccount: %w", err)
	}
	return &Account{
		Address:       ba.Address,
		AccountNumber: ba.AccountNumber,
		Sequence:      ba.Sequence,
	}, nil
}

func decodeEthAccount(value []byte) (*Account, error) {
	var ea ethermintv1.EthAccount
	if err := proto.Unmarshal(value, &ea); err != nil {
		return nil, fmt.Errorf("tx: decode EthAccount: %w", err)
	}
	ba := ea.GetBaseAccount()
	if ba == nil {
		return nil, fmt.Errorf("tx: EthAccount has no BaseAccount")
	}
	return &Account{
		Address:       ba.Address,
		AccountNumber: ba.AccountNumber,
		Sequence:      ba.Sequence,
	}, nil
}
