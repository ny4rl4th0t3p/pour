package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
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

	snap, ok := h.source.GetActive(req.ChainID)
	if !ok {
		pourRequestsTotal.WithLabelValues(req.ChainID, "chain_not_found").Inc()
		writeError(w, http.StatusNotFound, "chain not found or not enabled")
		return
	}

	if err := tx.ValidateAddress(req.Address, snap.Info.Bech32Prefix); err != nil {
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
		slog.Error("rate limit check failed", "error", err, "ip", ip, "chain_id", req.ChainID) //nolint:gosec // ip sanitized by net.SplitHostPort
		writeError(w, http.StatusInternalServerError, "rate limit check failed")
		return
	}

	coin, err := config.ParseCoin(snap.Drip.Anonymous)
	if err != nil {
		slog.ErrorContext(r.Context(), "pour: invalid drip config", "chain_id", req.ChainID, "error", err)
		writeError(w, http.StatusInternalServerError, "invalid drip config")
		return
	}

	coins := tx.Coins{{Amount: coin.Amount, Denom: coin.Denom}}
	amount := fmt.Sprintf("%s%s", coin.Amount, coin.Denom)
	now := time.Now().Unix()

	if h.pourer != nil {
		if handled := h.tryPoolRoute(w, r, req.ChainID, req.Address, coins, amount, ip, now); handled {
			return
		}
	}

	h.pourSync(w, r, req.ChainID, req.Address, coins, amount, ip, now)
}

// tryPoolRoute routes the request to the batch pool. Returns true when the request
// is fully handled (queued or error response sent), or false when ErrSyncMode is
// returned and the caller should fall through to the sync path.
func (h *Handler) tryPoolRoute(
	w http.ResponseWriter, r *http.Request,
	chainID, address string, coins tx.Coins, amount, ip string, now int64,
) bool {
	ch := make(chan batch.Result, 1)
	routeErr := h.pourer.Pour(chainID, batch.Request{
		ToAddress: address,
		Coins:     coins,
		Result:    ch,
	})

	if routeErr == nil {
		dripID, recordErr := h.dripStore.RecordDrip(r.Context(), store.DripRecord{
			ChainID:     chainID,
			Address:     address,
			Coins:       amount,
			RequesterIP: ip,
			Status:      pourapi.StatusQueued,
			RequestedAt: now,
		})
		if recordErr != nil {
			slog.ErrorContext(r.Context(), "pour: record queued drip", "error", recordErr)
		}
		pourRequestsTotal.WithLabelValues(chainID, "queued").Inc()
		writeJSON(w, http.StatusAccepted, pourapi.PourResponse{
			DripID: dripID,
			Status: pourapi.StatusQueued,
			Amount: amount,
		})
		// context.WithoutCancel detaches from the request so the goroutine outlives the response
		// while still carrying request-scoped values (e.g. trace IDs).
		go h.awaitDrip(context.WithoutCancel(r.Context()), chainID, dripID, ch)
		return true
	}

	if errors.Is(routeErr, chain.ErrSyncMode) {
		return false
	}
	if errors.Is(routeErr, chain.ErrChainSuspended) {
		pourRequestsTotal.WithLabelValues(chainID, "suspended").Inc()
		writeError(w, http.StatusServiceUnavailable, "chain is suspended")
		return true
	}
	pourRequestsTotal.WithLabelValues(chainID, "queue_full").Inc()
	writeError(w, http.StatusServiceUnavailable, "faucet busy: try again shortly")
	return true
}

// awaitDrip blocks until the batch result arrives (or the wait times out), then
// updates the drip record and emits the outcome metric.
// ctx should be context.WithoutCancel(r.Context()) so it outlives the response.
func (h *Handler) awaitDrip(ctx context.Context, chainID string, dripID int64, ch <-chan batch.Result) {
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer waitCancel()

	var res batch.Result
	select {
	case res = <-ch:
	case <-waitCtx.Done():
		res = batch.Result{Err: waitCtx.Err()}
	}

	// ctx (WithoutCancel parent) is still valid even if waitCtx expired.
	writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
	defer writeCancel()

	completedAt := time.Now().Unix()
	if res.Err != nil {
		pourRequestsTotal.WithLabelValues(chainID, "failed").Inc()
		if err := h.dripStore.UpdateDrip(writeCtx, dripID, pourapi.StatusFailed, "", completedAt); err != nil {
			slog.ErrorContext(ctx, "awaitDrip: update drip failed", "drip_id", dripID, "error", err)
		}
		return
	}
	pourRequestsTotal.WithLabelValues(chainID, "confirmed").Inc()
	if err := h.dripStore.UpdateDrip(writeCtx, dripID, pourapi.StatusConfirmed, res.TxHash, completedAt); err != nil {
		slog.ErrorContext(ctx, "awaitDrip: update drip confirmed", "drip_id", dripID, "error", err)
	}
}

// pourSync handles the synchronous broadcast path (batch_window = "0" or Pourer not set).
func (h *Handler) pourSync(w http.ResponseWriter, r *http.Request, chainID, address string, coins tx.Coins, amount, ip string, now int64) {
	bc, ok := h.broadcasters[chainID]
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "chain broadcaster not available")
		return
	}

	result, err := bc.BuildAndBroadcast(r.Context(), tx.SendRequest{
		KeyIndex:  0,
		ToAddress: address,
		Coins:     coins,
	})
	if err != nil {
		pourRequestsTotal.WithLabelValues(chainID, "broadcast_error").Inc()
		slog.ErrorContext(r.Context(), "pour: broadcast", "chain", chainID, "error", err)
		writeError(w, http.StatusInternalServerError, "broadcast failed")
		return
	}

	dripID, err := h.dripStore.RecordDrip(r.Context(), store.DripRecord{
		ChainID:     chainID,
		Address:     address,
		Coins:       amount,
		RequesterIP: ip,
		TxHash:      result.TxHash,
		Status:      pourapi.StatusConfirmed,
		RequestedAt: now,
		CompletedAt: now,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "pour: record drip", "tx_hash", result.TxHash, "error", err)
	}

	pourRequestsTotal.WithLabelValues(chainID, "confirmed").Inc()
	writeJSON(w, http.StatusOK, pourapi.PourResponse{
		DripID: dripID,
		Status: pourapi.StatusConfirmed,
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
