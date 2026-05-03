package tx

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	ethermintv1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/ethermint/types/v1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
)

func TestQueryAccount_baseAccount(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{
		Address:       testFromAddr,
		AccountNumber: 42,
		Sequence:      7,
	})

	client := authv1beta1.NewQueryClient(conn)
	acc, err := queryAccount(t.Context(), client, testFromAddr)
	if err != nil {
		t.Fatalf("queryAccount: %v", err)
	}
	if acc.AccountNumber != 42 {
		t.Errorf("AccountNumber: got %d, want 42", acc.AccountNumber)
	}
	if acc.Sequence != 7 {
		t.Errorf("Sequence: got %d, want 7", acc.Sequence)
	}
}

func TestQueryAccount_notFound(t *testing.T) {
	conn := fakechain.Start(t, fakechain.Config{Address: testFromAddr})
	client := authv1beta1.NewQueryClient(conn)

	_, err := queryAccount(t.Context(), client, "osmo1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestDecodeEthAccount(t *testing.T) {
	ba := &authv1beta1.BaseAccount{
		Address:       testFromAddr,
		AccountNumber: 10,
		Sequence:      3,
	}
	ea := &ethermintv1.EthAccount{BaseAccount: ba}
	b, err := proto.Marshal(ea)
	if err != nil {
		t.Fatalf("marshal EthAccount: %v", err)
	}

	acc, err := decodeEthAccount(b)
	if err != nil {
		t.Fatalf("decodeEthAccount: %v", err)
	}
	if acc.AccountNumber != 10 || acc.Sequence != 3 {
		t.Errorf("got AccountNumber=%d Sequence=%d", acc.AccountNumber, acc.Sequence)
	}
}

func TestDecodeUnknownAccountType(t *testing.T) {
	// Decoder map is keyed on TypeUrl; an unknown URL falls through to BaseAccount
	// unmarshal. Pass garbage bytes to force ErrUnknownAccountType.
	_, err := decodeBaseAccount([]byte{0xFF, 0xFE})
	if err == nil {
		t.Fatal("expected error decoding garbage bytes")
	}
}

func TestKnownAccountDecoders_ethermintTypeURL(t *testing.T) {
	ba := &authv1beta1.BaseAccount{Address: testFromAddr, AccountNumber: 5, Sequence: 1}
	ea := &ethermintv1.EthAccount{BaseAccount: ba}
	b, _ := proto.Marshal(ea)

	ethAny := &anypb.Any{TypeUrl: "/ethermint.types.v1.EthAccount", Value: b}
	dec, ok := knownAccountDecoders[ethAny.TypeUrl]
	if !ok {
		t.Fatal("no decoder for /ethermint.types.v1.EthAccount")
	}
	acc, err := dec(ethAny.Value)
	if err != nil {
		t.Fatalf("decoder: %v", err)
	}
	if acc.AccountNumber != 5 {
		t.Errorf("AccountNumber: got %d, want 5", acc.AccountNumber)
	}
}
