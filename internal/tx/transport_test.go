package tx

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsUnavailable_grpcUnavailable(t *testing.T) {
	// Wrapped gRPC Unavailable, as returned by queryAccountGRPC on connection failure.
	grpcErr := status.Error(codes.Unavailable, "connection refused")
	wrapped := fmt.Errorf("tx: query account: %w", grpcErr)
	if !isUnavailable(wrapped) {
		t.Errorf("expected true for wrapped gRPC Unavailable, got false")
	}
}

func TestIsUnavailable_restSentinel(t *testing.T) {
	err := fmt.Errorf("GET /foo: %w", errRESTUnavailable)
	if !isUnavailable(err) {
		t.Errorf("expected true for wrapped errRESTUnavailable, got false")
	}
}

func TestIsUnavailable_otherGRPCCode(t *testing.T) {
	err := status.Error(codes.NotFound, "not found")
	if isUnavailable(err) {
		t.Errorf("expected false for codes.NotFound, got true")
	}
}

func TestIsUnavailable_plainError(t *testing.T) {
	if isUnavailable(fmt.Errorf("some other error")) {
		t.Errorf("expected false for plain error, got true")
	}
}
