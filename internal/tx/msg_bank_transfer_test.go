package tx

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	transferv1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/ibc/applications/transfer/v1"
)

func TestBuildMsgTransfer_TypeURL(t *testing.T) {
	msgAny, err := buildMsgTransfer(
		"cosmos1sender", "osmo1receiver",
		"transfer", "channel-141",
		Coin{Denom: "uatom", Amount: "1000000"},
		1_700_000_000_000_000_000, // timeout_timestamp
		"",
	)
	if err != nil {
		t.Fatalf("buildMsgTransfer: %v", err)
	}
	if msgAny.TypeUrl != "/ibc.applications.transfer.v1.MsgTransfer" {
		t.Errorf("TypeUrl = %q, want /ibc.applications.transfer.v1.MsgTransfer", msgAny.TypeUrl)
	}
}

func TestBuildMsgTransfer_RoundTrip(t *testing.T) {
	msgAny, err := buildMsgTransfer(
		"cosmos1sender", "osmo1receiver",
		"transfer", "channel-141",
		Coin{Denom: "uatom", Amount: "1000000"},
		1_700_000_000_000_000_000,
		"test memo",
	)
	if err != nil {
		t.Fatalf("buildMsgTransfer: %v", err)
	}

	var msg transferv1.MsgTransfer
	if err := proto.Unmarshal(msgAny.Value, &msg); err != nil {
		t.Fatalf("unmarshal MsgTransfer: %v", err)
	}

	if msg.SourcePort != "transfer" {
		t.Errorf("SourcePort = %q, want transfer", msg.SourcePort)
	}
	if msg.SourceChannel != "channel-141" {
		t.Errorf("SourceChannel = %q, want channel-141", msg.SourceChannel)
	}
	if msg.Sender != "cosmos1sender" {
		t.Errorf("Sender = %q, want cosmos1sender", msg.Sender)
	}
	if msg.Receiver != "osmo1receiver" {
		t.Errorf("Receiver = %q, want osmo1receiver", msg.Receiver)
	}
	if msg.Token == nil || msg.Token.Denom != "uatom" || msg.Token.Amount != "1000000" {
		t.Errorf("Token = %v, want uatom/1000000", msg.Token)
	}
	if msg.TimeoutTimestamp != 1_700_000_000_000_000_000 {
		t.Errorf("TimeoutTimestamp = %d, want 1700000000000000000", msg.TimeoutTimestamp)
	}
	if msg.Memo != "test memo" {
		t.Errorf("Memo = %q, want test memo", msg.Memo)
	}
	// timeout_height must be nil (we use timestamp only).
	if msg.TimeoutHeight != nil && (msg.TimeoutHeight.RevisionNumber != 0 || msg.TimeoutHeight.RevisionHeight != 0) {
		t.Errorf("TimeoutHeight = %v, want nil/zero", msg.TimeoutHeight)
	}
}

func TestBuildMsgTransfer_Deterministic(t *testing.T) {
	sender := "cosmos1sender"
	receiver := "osmo1receiver"
	port := "transfer"
	channel := "channel-0"
	token := Coin{Denom: "uosmo", Amount: "500000"}
	timeout := uint64(9999999999)
	memo := ""

	a, _ := buildMsgTransfer(sender, receiver, port, channel, token, timeout, memo)
	b, _ := buildMsgTransfer(sender, receiver, port, channel, token, timeout, memo)
	if !bytes.Equal(a.Value, b.Value) {
		t.Error("buildMsgTransfer is not deterministic")
	}
}
