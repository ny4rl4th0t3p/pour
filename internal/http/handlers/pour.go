package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Pour handles POST /v1/pour.
func (h *Handler) Pour(w http.ResponseWriter, r *http.Request) {
	var req pourapi.PourRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		pourRequestsTotal.WithLabelValues("", "bad_json").Inc()
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	chain, ok := h.chains[req.ChainID]
	if !ok {
		pourRequestsTotal.WithLabelValues(req.ChainID, "chain_not_found").Inc()
		writeError(w, http.StatusNotFound, "chain not found or not enabled")
		return
	}

	if err := tx.ValidateAddress(req.Address, chain.Info.Bech32Prefix); err != nil {
		pourRequestsTotal.WithLabelValues(req.ChainID, "invalid_address").Inc()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ip := extractIP(r.RemoteAddr)
	if err := h.limiter.Check(r.Context(), ip, req.ChainID); err != nil {
		var rl *ratelimit.ErrRateLimitExceeded
		if errors.As(err, &rl) {
			pourRequestsTotal.WithLabelValues(req.ChainID, "rate_limited").Inc()
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(rl.RetryAfter.Seconds()))))
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "rate limit check failed")
		return
	}

	coin, err := config.ParseCoin(chain.Drip.Anonymous)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid drip config")
		return
	}

	bc, ok := h.broadcasters[req.ChainID]
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "chain broadcaster not available")
		return
	}

	result, err := bc.BuildAndBroadcast(r.Context(), tx.SendRequest{
		Mnemonic:  h.mnemonic,
		KeyIndex:  0,
		ToAddress: req.Address,
		Coins:     tx.Coins{{Amount: coin.Amount, Denom: coin.Denom}},
		GasCache:  h.gasCache,
	})
	if err != nil {
		pourRequestsTotal.WithLabelValues(req.ChainID, "broadcast_error").Inc()
		slog.ErrorContext(r.Context(), "pour: broadcast", "chain", req.ChainID, "error", err)
		writeError(w, http.StatusInternalServerError, "broadcast failed")
		return
	}

	amount := fmt.Sprintf("%s%s", coin.Amount, coin.Denom)
	now := time.Now().Unix()
	dripID, err := h.dripStore.RecordDrip(r.Context(), store.DripRecord{
		ChainID:     req.ChainID,
		Address:     req.Address,
		Coins:       amount,
		RequesterIP: ip,
		TxHash:      result.TxHash,
		Status:      "confirmed",
		RequestedAt: now,
		CompletedAt: now,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "pour: record drip", "tx_hash", result.TxHash, "error", err)
		// tokens were already sent; return success with drip_id 0
	}

	pourRequestsTotal.WithLabelValues(req.ChainID, "confirmed").Inc()
	writeJSON(w, http.StatusOK, pourapi.PourResponse{
		DripID: dripID,
		Status: "confirmed",
		Amount: amount,
		TxHash: result.TxHash,
	})
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
