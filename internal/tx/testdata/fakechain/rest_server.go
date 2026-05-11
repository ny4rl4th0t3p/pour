package fakechain

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// StartREST starts an httptest server that mirrors the Cosmos REST (LCD) API using cfg.
// The returned string is the server's base URL (e.g. "http://127.0.0.1:PORT").
// The server is stopped when t.Cleanup runs.
func StartREST(t *testing.T, cfg Config) string {
	t.Helper()
	s := &fakeRESTServer{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cosmos/auth/v1beta1/accounts/{address}", s.handleAccount)
	mux.HandleFunc("POST /cosmos/tx/v1beta1/simulate", s.handleSimulate)
	mux.HandleFunc("POST /cosmos/tx/v1beta1/txs", s.handleBroadcast)
	mux.HandleFunc("GET /cosmos/tx/v1beta1/txs/{hash}", s.handleGetTx)
	mux.HandleFunc("GET /cosmos/bank/v1beta1/balances/{address}/by_denom", s.handleBalance)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

type fakeRESTServer struct {
	cfg            Config
	mu             sync.Mutex
	accountCalls   int
	broadcastCalls int
	getTxCalls     int
}

func (s *fakeRESTServer) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *fakeRESTServer) handleAccount(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Unavailable {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	address := r.PathValue("address")
	if address != s.cfg.Address {
		http.Error(w, fmt.Sprintf(`{"code":5,"message":"account not found: %s"}`, address), http.StatusNotFound)
		return
	}

	s.mu.Lock()
	seq := s.cfg.Sequence
	if len(s.cfg.SequencesPerQuery) > 0 {
		idx := s.accountCalls
		if idx >= len(s.cfg.SequencesPerQuery) {
			idx = len(s.cfg.SequencesPerQuery) - 1
		}
		seq = s.cfg.SequencesPerQuery[idx]
		s.accountCalls++
	}
	s.mu.Unlock()

	s.writeJSON(w, map[string]any{
		"account": map[string]any{
			"@type":          "/cosmos.auth.v1beta1.BaseAccount",
			"address":        s.cfg.Address,
			"account_number": fmt.Sprintf("%d", s.cfg.AccountNumber),
			"sequence":       fmt.Sprintf("%d", seq),
		},
	})
}

func (s *fakeRESTServer) handleSimulate(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Unavailable {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.cfg.GasUsed == 0 {
		http.Error(w, `{"code":12,"message":"simulate not configured"}`, http.StatusNotImplemented)
		return
	}
	s.writeJSON(w, map[string]any{
		"gas_info": map[string]any{
			"gas_used":   fmt.Sprintf("%d", s.cfg.GasUsed),
			"gas_wanted": "0",
		},
	})
}

func (s *fakeRESTServer) handleBroadcast(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Unavailable {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}

	s.mu.Lock()
	code := s.cfg.BroadcastCode
	if len(s.cfg.BroadcastCodes) > 0 {
		idx := s.broadcastCalls
		if idx >= len(s.cfg.BroadcastCodes) {
			idx = len(s.cfg.BroadcastCodes) - 1
		}
		code = s.cfg.BroadcastCodes[idx]
		s.broadcastCalls++
	}
	s.mu.Unlock()

	s.writeJSON(w, map[string]any{
		"tx_response": map[string]any{
			"code":    code,
			"txhash":  s.cfg.BroadcastTxHash,
			"raw_log": "",
		},
	})
}

func (s *fakeRESTServer) handleGetTx(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Unavailable {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	hash := r.PathValue("hash")
	if hash != s.cfg.BroadcastTxHash {
		http.Error(w, fmt.Sprintf(`{"code":5,"message":"tx not found: %s"}`, hash), http.StatusNotFound)
		return
	}

	s.mu.Lock()
	s.getTxCalls++
	calls := s.getTxCalls
	s.mu.Unlock()

	if calls <= s.cfg.ConfirmAfter {
		http.Error(w, `{"code":5,"message":"not yet confirmed"}`, http.StatusNotFound)
		return
	}

	s.writeJSON(w, map[string]any{
		"tx_response": map[string]any{
			"code":     0,
			"txhash":   s.cfg.BroadcastTxHash,
			"height":   fmt.Sprintf("%d", s.cfg.TxHeight),
			"gas_used": fmt.Sprintf("%d", s.cfg.GetTxGasUsed),
			"raw_log":  "",
		},
	})
}

func (s *fakeRESTServer) handleBalance(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Unavailable {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	denom := r.URL.Query().Get("denom")
	amount := s.cfg.BalanceAmount
	if amount == "" {
		amount = "0"
	}
	s.writeJSON(w, map[string]any{
		"balance": map[string]any{
			"denom":  denom,
			"amount": amount,
		},
	})
}
