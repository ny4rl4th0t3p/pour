package tx

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
	basev1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/v1beta1"
	cryptosecp256k1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/crypto/secp256k1"
	signingv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/signing/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
)

// buildTxRaw assembles and signs a TxRaw.
// Field order and Deterministic marshaling are load-bearing for golden byte equality.
func buildTxRaw(
	privKey *keys.PrivKey,
	account Account,
	msgs []*anypb.Any,
	estimate Estimate,
	chainID string,
) (*txv1beta1.TxRaw, error) {
	// 1. TxBody
	body := &txv1beta1.TxBody{
		Messages: msgs,
	}
	bodyBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal TxBody: %w", err)
	}

	// 2. Public key as proto Any
	pubKeyMsg := &cryptosecp256k1.PubKey{Key: privKey.PubKey().Bytes()}
	pubKeyBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(pubKeyMsg)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal secp256k1 PubKey: %w", err)
	}
	pubKeyAny := &anypb.Any{
		TypeUrl: keys.PubKeyAnyTypeURL(),
		Value:   pubKeyBytes,
	}

	// 3. AuthInfo
	authInfo := &txv1beta1.AuthInfo{
		SignerInfos: []*txv1beta1.SignerInfo{
			{
				PublicKey: pubKeyAny,
				ModeInfo: &txv1beta1.ModeInfo{
					Sum: &txv1beta1.ModeInfo_Single_{
						Single: &txv1beta1.ModeInfo_Single{
							Mode: signingv1beta1.SignMode_SIGN_MODE_DIRECT,
						},
					},
				},
				Sequence: account.Sequence,
			},
		},
		Fee: &txv1beta1.Fee{
			Amount:   []*basev1beta1.Coin{{Denom: estimate.Fee.Denom, Amount: estimate.Fee.Amount}},
			GasLimit: estimate.GasLimit,
		},
	}
	authInfoBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(authInfo)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal AuthInfo: %w", err)
	}

	// 4. SignDoc → sign
	signDoc := &txv1beta1.SignDoc{
		BodyBytes:     bodyBytes,
		AuthInfoBytes: authInfoBytes,
		ChainId:       chainID,
		AccountNumber: account.AccountNumber,
	}
	signDocBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(signDoc)
	if err != nil {
		return nil, fmt.Errorf("tx: marshal SignDoc: %w", err)
	}

	// PrivKey.Sign SHA256-hashes internally and returns 64-byte compact R‖S.
	sig, err := privKey.Sign(signDocBytes)
	if err != nil {
		return nil, fmt.Errorf("tx: sign: %w", err)
	}

	return &txv1beta1.TxRaw{
		BodyBytes:     bodyBytes,
		AuthInfoBytes: authInfoBytes,
		Signatures:    [][]byte{sig},
	}, nil
}
