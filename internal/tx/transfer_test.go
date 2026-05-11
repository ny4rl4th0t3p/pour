package tx

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	abciv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/abci/v1beta1"
	"github.com/ny4rl4th0t3p/pour/internal/tx/testdata/fakechain"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

func TestBuildAndBroadcastTransfer_HappyPath(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	conn := fakechain.StartGRPC(t, fakechain.Config{
		Address:         testFromAddr,
		AccountNumber:   1,
		Sequence:        0,
		GasUsed:         150_000,
		BroadcastTxHash: testTxHash,
		TxHeight:        10,
		GetTxPacketSeq:  42,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "cosmoshub-4",
		Bech32Prefix: "osmo", // matches testFromAddr prefix
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uatom", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	result, err := newTestClient(t, conn, chain).BuildAndBroadcastTransfer(t.Context(), TransferRequest{
		KeyIndex:         0,
		SourcePort:       "transfer",
		SourceChannel:    "channel-141",
		Token:            Coin{Denom: "uatom", Amount: "1000000"},
		ReceiverAddress:  "osmo1receiver",
		TimeoutTimestamp: uint64(time.Now().Add(10 * time.Minute).UnixNano()),
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcastTransfer: %v", err)
	}
	if result.TxHash != testTxHash {
		t.Errorf("TxHash = %q, want %q", result.TxHash, testTxHash)
	}
	if result.Height != 10 {
		t.Errorf("Height = %d, want 10", result.Height)
	}
	if result.PacketSequence != 42 {
		t.Errorf("PacketSequence = %d, want 42", result.PacketSequence)
	}
}

func TestBuildAndBroadcastTransfer_MissingPacketSeq(t *testing.T) {
	origInterval := confirmPollInterval
	confirmPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { confirmPollInterval = origInterval })

	// GetTxPacketSeq=0 means no send_packet log is injected.
	conn := fakechain.StartGRPC(t, fakechain.Config{
		Address:         testFromAddr,
		GasUsed:         150_000,
		BroadcastTxHash: testTxHash,
		TxHeight:        1,
	})

	chain := &chainregistry.ChainInfo{
		ChainID:      "cosmoshub-4",
		Bech32Prefix: "osmo",
		Slip44:       118,
		FeeTokens:    []chainregistry.FeeToken{{Denom: "uatom", AverageGasPrice: decimal.NewFromFloat(0.025)}},
	}

	result, err := newTestClient(t, conn, chain).BuildAndBroadcastTransfer(t.Context(), TransferRequest{
		KeyIndex:         0,
		SourcePort:       "transfer",
		SourceChannel:    "channel-0",
		Token:            Coin{Denom: "uatom", Amount: "500000"},
		ReceiverAddress:  "osmo1receiver",
		TimeoutTimestamp: uint64(time.Now().Add(10 * time.Minute).UnixNano()),
	})
	if err != nil {
		t.Fatalf("BuildAndBroadcastTransfer: %v", err)
	}
	if result.PacketSequence != 0 {
		t.Errorf("PacketSequence = %d, want 0 when no send_packet log", result.PacketSequence)
	}
}

func TestExtractPacketSequence(t *testing.T) {
	logs := []*abciv1beta1.ABCIMessageLog{{
		Events: []*abciv1beta1.StringEvent{
			{
				Type: "coin_received",
				Attributes: []*abciv1beta1.Attribute{
					{Key: "receiver", Value: "cosmos1abc"},
				},
			},
			{
				Type: "send_packet",
				Attributes: []*abciv1beta1.Attribute{
					{Key: "packet_channel_ordering", Value: "ORDER_UNORDERED"},
					{Key: "packet_sequence", Value: "7"},
					{Key: "packet_src_channel", Value: "channel-141"},
				},
			},
		},
	}}

	if seq := extractPacketSequence(logs); seq != 7 {
		t.Errorf("extractPacketSequence = %d, want 7", seq)
	}
}

func TestExtractPacketSequence_NotFound(t *testing.T) {
	logs := []*abciv1beta1.ABCIMessageLog{{
		Events: []*abciv1beta1.StringEvent{{
			Type:       "coin_received",
			Attributes: []*abciv1beta1.Attribute{{Key: "receiver", Value: "cosmos1abc"}},
		}},
	}}
	if seq := extractPacketSequence(logs); seq != 0 {
		t.Errorf("extractPacketSequence = %d, want 0 when no send_packet event", seq)
	}
}

func TestExtractPacketSequence_EmptyLogs(t *testing.T) {
	if seq := extractPacketSequence(nil); seq != 0 {
		t.Errorf("extractPacketSequence(nil) = %d, want 0", seq)
	}
}
