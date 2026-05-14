package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// StartMockRegistry starts an httptest server that serves:
//   - /hub/chain.json  — real gRPC address from chainA
//   - /mynet/chain.json   — real gRPC address from chainB, or a dead placeholder if chainB is nil
//   - /_IBC/hub-mynet.json — one synthetic ICS20 channel (channel-0 ↔ channel-0)
//
// Returns the server base URL.
func StartMockRegistry(t *testing.T, chainA, chainB *SimappChain) string {
	t.Helper()

	chainBGRPC := "127.0.0.1:19090"
	if chainB != nil {
		chainBGRPC = chainB.GRPCAddr
	}
	chainAJSON := chainJSON("hub", "hub-1", "cosmos", "stake", chainA.GRPCAddr)
	chainBJSON := chainJSON("mynet", "mynet-1", "cosmos", "uosmo", chainBGRPC)
	ibcJSON := ibcFileJSON("hub", "mynet")

	mux := http.NewServeMux()
	mux.HandleFunc("/hub/chain.json", serveJSON(chainAJSON))
	mux.HandleFunc("/mynet/chain.json", serveJSON(chainBJSON))
	mux.HandleFunc("/_IBC/hub-mynet.json", serveJSON(ibcJSON))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func serveJSON(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func chainJSON(chainName, chainID, prefix, denom, grpcAddr string) []byte {
	v := map[string]any{
		"chain_name":    chainName,
		"chain_id":      chainID,
		"bech32_prefix": prefix,
		"slip44":        118,
		"key_algos":     []string{"secp256k1"},
		"fees": map[string]any{
			"fee_tokens": []map[string]any{
				{"denom": denom, "average_gas_price": 0.025},
			},
		},
		"apis": map[string]any{
			"grpc": []map[string]any{
				{"address": grpcAddr},
			},
		},
	}
	data, _ := json.Marshal(v)
	return data
}

func ibcFileJSON(nameA, nameB string) []byte {
	v := map[string]any{
		"chain_1": map[string]any{
			"chain_name":    nameA,
			"client_id":     "07-tendermint-0",
			"connection_id": "connection-0",
		},
		"chain_2": map[string]any{
			"chain_name":    nameB,
			"client_id":     "07-tendermint-0",
			"connection_id": "connection-0",
		},
		"channels": []map[string]any{
			{
				"chain_1":  map[string]any{"channel_id": "channel-0", "port_id": "transfer"},
				"chain_2":  map[string]any{"channel_id": "channel-0", "port_id": "transfer"},
				"ordering": "unordered",
				"version":  "ics20-1",
				"tags":     map[string]any{"status": "live", "preferred": true},
			},
		},
	}
	data, _ := json.Marshal(v)
	return data
}
