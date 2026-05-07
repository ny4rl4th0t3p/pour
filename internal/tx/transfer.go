package tx

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
	abciv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/abci/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
)

// BuildAndBroadcastTransfer signs and broadcasts a single MsgTransfer.
// Returns TransferResult with the IBC packet sequence extracted from the
// tx event logs (send_packet.packet_sequence). PacketSequence is 0 if the
// attribute is absent or unparseable.
func (c *Client) BuildAndBroadcastTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	privKey, ok := c.cachedKeys[req.KeyIndex]
	if !ok {
		return TransferResult{}, fmt.Errorf("tx: key index %d not pre-derived", req.KeyIndex)
	}
	fromAddr, err := keys.AddressFromPubKey(privKey.PubKey(), c.chain.Bech32Prefix)
	if err != nil {
		return TransferResult{}, fmt.Errorf("tx: derive address: %w", err)
	}

	msgAny, err := buildMsgTransfer(
		fromAddr, req.ReceiverAddress,
		req.SourcePort, req.SourceChannel,
		req.Token, req.TimeoutTimestamp, req.Memo,
	)
	if err != nil {
		return TransferResult{}, err
	}

	base, err := c.doSend(ctx, privKey, fromAddr, []*anypb.Any{msgAny}, MsgTypeTransfer, 1)
	if err != nil {
		return TransferResult{}, err
	}

	return TransferResult{
		TxHash:         base.TxHash,
		Height:         base.Height,
		GasUsed:        base.GasUsed,
		PacketSequence: c.fetchPacketSequence(ctx, base.TxHash),
	}, nil
}

// fetchPacketSequence queries GetTx for txHash and extracts the IBC packet
// sequence from the committed tx logs. Returns 0 on any failure.
func (c *Client) fetchPacketSequence(ctx context.Context, txHash string) uint64 {
	resp, err := c.bundle.txSvc.GetTx(ctx, &txv1beta1.GetTxRequest{Hash: txHash})
	if err != nil || resp.GetTxResponse() == nil {
		return 0
	}
	return extractPacketSequence(resp.GetTxResponse().GetLogs())
}

// extractPacketSequence walks ABCIMessageLog events looking for a send_packet
// event with a packet_sequence attribute. Returns 0 if not found.
func extractPacketSequence(logs []*abciv1beta1.ABCIMessageLog) uint64 {
	for _, log := range logs {
		for _, event := range log.GetEvents() {
			if event.GetType() != "send_packet" {
				continue
			}
			for _, attr := range event.GetAttributes() {
				if attr.GetKey() == "packet_sequence" {
					n, err := strconv.ParseUint(attr.GetValue(), 10, 64)
					if err == nil {
						return n
					}
				}
			}
		}
	}
	return 0
}
