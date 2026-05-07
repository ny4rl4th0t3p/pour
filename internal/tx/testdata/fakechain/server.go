// Package fakechain provides a scriptable in-process gRPC server for testing internal/tx.
package fakechain

import (
	"context"
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	authv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/auth/v1beta1"
	abciv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/base/abci/v1beta1"
	txv1beta1 "github.com/ny4rl4th0t3p/pour/internal/tx/internal/proto/cosmos/tx/v1beta1"
)

const bufSize = 1 << 20 // 1 MiB

// Config scripts what the fake server returns. Zero values mean "return an error / not found".
type Config struct {
	// Account query
	AccountNumber uint64
	Sequence      uint64
	Address       string // must match the queried address

	// Simulate
	GasUsed uint64 // 0 → return Unimplemented

	// BroadcastTx
	BroadcastTxHash string // returned on success
	BroadcastCode   uint32 // non-zero → error response

	// GetTx: confirmed after ConfirmAfter calls (0 = immediately)
	ConfirmAfter   int
	TxHeight       int64
	GetTxGasUsed   int64  // gas_used in the confirmed TxResponse
	GetTxPacketSeq uint64 // if non-zero, inject a send_packet log with this sequence
}

// Start registers the fake servers, starts a bufconn listener, and returns a
// client connection. The server is stopped when t.Cleanup runs.
func Start(t *testing.T, cfg Config) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()

	authv1beta1.RegisterQueryServer(srv, &fakeAuth{cfg: &cfg})
	txv1beta1.RegisterServiceServer(srv, &fakeTxSvc{cfg: &cfg})

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		lis.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("fakechain: dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// ---- auth query ----

type fakeAuth struct {
	authv1beta1.UnimplementedQueryServer
	cfg *Config
}

func (f *fakeAuth) Account(_ context.Context, req *authv1beta1.QueryAccountRequest) (*authv1beta1.QueryAccountResponse, error) {
	if req.Address != f.cfg.Address {
		return nil, status.Errorf(codes.NotFound, "account not found: %s", req.Address)
	}
	ba := &authv1beta1.BaseAccount{
		Address:       f.cfg.Address,
		AccountNumber: f.cfg.AccountNumber,
		Sequence:      f.cfg.Sequence,
	}
	b, err := proto.Marshal(ba)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal: %v", err)
	}
	return &authv1beta1.QueryAccountResponse{
		Account: &anypb.Any{
			TypeUrl: "/cosmos.auth.v1beta1.BaseAccount",
			Value:   b,
		},
	}, nil
}

// ---- tx service ----

type fakeTxSvc struct {
	txv1beta1.UnimplementedServiceServer
	cfg        *Config
	getTxCalls int
}

func (f *fakeTxSvc) Simulate(_ context.Context, _ *txv1beta1.SimulateRequest) (*txv1beta1.SimulateResponse, error) {
	if f.cfg.GasUsed == 0 {
		return nil, status.Error(codes.Unimplemented, "simulate not configured")
	}
	return &txv1beta1.SimulateResponse{
		GasInfo: &abciv1beta1.GasInfo{GasUsed: f.cfg.GasUsed},
	}, nil
}

func (f *fakeTxSvc) BroadcastTx(_ context.Context, _ *txv1beta1.BroadcastTxRequest) (*txv1beta1.BroadcastTxResponse, error) {
	return &txv1beta1.BroadcastTxResponse{
		TxResponse: &abciv1beta1.TxResponse{
			Txhash: f.cfg.BroadcastTxHash,
			Code:   f.cfg.BroadcastCode,
		},
	}, nil
}

func (f *fakeTxSvc) GetTx(_ context.Context, req *txv1beta1.GetTxRequest) (*txv1beta1.GetTxResponse, error) {
	if req.Hash != f.cfg.BroadcastTxHash {
		return nil, status.Errorf(codes.NotFound, "tx not found: %s", req.Hash)
	}
	f.getTxCalls++
	if f.getTxCalls <= f.cfg.ConfirmAfter {
		return nil, status.Error(codes.NotFound, "not yet confirmed")
	}
	txResp := &abciv1beta1.TxResponse{
		Txhash:  f.cfg.BroadcastTxHash,
		Height:  f.cfg.TxHeight,
		GasUsed: f.cfg.GetTxGasUsed,
		Code:    0,
	}
	if f.cfg.GetTxPacketSeq != 0 {
		txResp.Logs = []*abciv1beta1.ABCIMessageLog{{
			Events: []*abciv1beta1.StringEvent{{
				Type: "send_packet",
				Attributes: []*abciv1beta1.Attribute{{
					Key:   "packet_sequence",
					Value: fmt.Sprintf("%d", f.cfg.GetTxPacketSeq),
				}},
			}},
		}}
	}
	return &txv1beta1.GetTxResponse{TxResponse: txResp}, nil
}
