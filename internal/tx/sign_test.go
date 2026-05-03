package tx

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ny4rl4th0t3p/pour/internal/tx/internal/keys"
)

// testMnemonic is the standard BIP39 all-zeros entropy mnemonic used across
// cosmjs, keplr, and cosmos-sdk test suites.
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

const (
	testFromAddr = "osmo19rl4cm2hmr8afy4kldpxz3fka4jguq0a5m7df8"
	testToAddr   = "osmo1qnk2n4nlkpw9xfqntladh74er2xa62wgas3elx"
	testChainID  = "osmosis-1"
)

// sigVector mirrors the shape of testdata/sigvectors/msgsend-osmosis.json.
type sigVector struct {
	TxBytesHex string `json:"tx_bytes_hex"`
}

// TestBuildTxRaw_goldenVector asserts byte-for-byte equality against the cosmjs-generated vector.
func TestBuildTxRaw_goldenVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/sigvectors/msgsend-osmosis.json")
	if err != nil {
		t.Fatalf("read vector: %v", err)
	}
	var vec sigVector
	if err := json.Unmarshal(raw, &vec); err != nil {
		t.Fatalf("parse vector: %v", err)
	}
	want, err := hex.DecodeString(vec.TxBytesHex)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}

	privKey, err := keys.DerivePrivKey(testMnemonic, 118, 0)
	if err != nil {
		t.Fatalf("DerivePrivKey: %v", err)
	}
	msgAny, err := buildMsgSend(testFromAddr, testToAddr, Coins{{Denom: "uosmo", Amount: "1000000"}})
	if err != nil {
		t.Fatalf("buildMsgSend: %v", err)
	}

	account := Account{Address: testFromAddr, AccountNumber: 1, Sequence: 0}
	estimate := Estimate{GasLimit: 200000, Fee: Coin{Denom: "uosmo", Amount: "5000"}}

	txRaw, err := buildTxRaw(privKey, account, []*anypb.Any{msgAny}, estimate, testChainID)
	if err != nil {
		t.Fatalf("buildTxRaw: %v", err)
	}
	got, err := proto.Marshal(txRaw)
	if err != nil {
		t.Fatalf("marshal TxRaw: %v", err)
	}

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("tx_bytes mismatch:\n  got:  %x\n  want: %x", got, want)
	}
}

// TestBuildTxRaw_structure verifies the TxRaw fields are populated correctly.
func TestBuildTxRaw(t *testing.T) {
	privKey, err := keys.DerivePrivKey(testMnemonic, 118, 0)
	if err != nil {
		t.Fatalf("DerivePrivKey: %v", err)
	}
	msgAny, err := buildMsgSend(testFromAddr, testToAddr, Coins{{Denom: "uosmo", Amount: "1000000"}})
	if err != nil {
		t.Fatalf("buildMsgSend: %v", err)
	}

	account := Account{Address: testFromAddr, AccountNumber: 1, Sequence: 0}
	estimate := Estimate{GasLimit: 200000, Fee: Coin{Denom: "uosmo", Amount: "5000"}}

	txRaw, err := buildTxRaw(privKey, account, []*anypb.Any{msgAny}, estimate, testChainID)
	if err != nil {
		t.Fatalf("buildTxRaw: %v", err)
	}

	if len(txRaw.BodyBytes) == 0 {
		t.Error("BodyBytes is empty")
	}
	if len(txRaw.AuthInfoBytes) == 0 {
		t.Error("AuthInfoBytes is empty")
	}
	if len(txRaw.Signatures) != 1 {
		t.Errorf("Signatures: got %d, want 1", len(txRaw.Signatures))
	}
	if len(txRaw.Signatures[0]) != 64 {
		t.Errorf("signature length: got %d, want 64", len(txRaw.Signatures[0]))
	}
}

// TestBuildTxRaw_deterministic verifies the same inputs always produce identical bytes.
func TestBuildTxRaw_deterministic(t *testing.T) {
	privKey, _ := keys.DerivePrivKey(testMnemonic, 118, 0)
	msgAny, _ := buildMsgSend(testFromAddr, testToAddr, Coins{{Denom: "uosmo", Amount: "1000000"}})
	account := Account{AccountNumber: 1, Sequence: 0}
	estimate := Estimate{GasLimit: 200000, Fee: Coin{Denom: "uosmo", Amount: "5000"}}
	msgs := []*anypb.Any{msgAny}

	a, err := buildTxRaw(privKey, account, msgs, estimate, testChainID)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	b, err := buildTxRaw(privKey, account, msgs, estimate, testChainID)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}

	if !bytes.Equal(a.BodyBytes, b.BodyBytes) {
		t.Error("BodyBytes not deterministic")
	}
	if !bytes.Equal(a.AuthInfoBytes, b.AuthInfoBytes) {
		t.Error("AuthInfoBytes not deterministic")
	}
	if !bytes.Equal(a.Signatures[0], b.Signatures[0]) {
		t.Error("Signature not deterministic")
	}
}
