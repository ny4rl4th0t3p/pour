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

	"github.com/ny4rl4th0t3p/pour/internal/abuse"
	"github.com/ny4rl4th0t3p/pour/internal/abuse/ratelimit"
	"github.com/ny4rl4th0t3p/pour/internal/batch"
	"github.com/ny4rl4th0t3p/pour/internal/chain"
	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/store"
	"github.com/ny4rl4th0t3p/pour/internal/tx"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
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

	// Route IBC drip when a specific denom is requested, or when chain has no native drip.
	if req.Denom != "" {
		ibcDrip, ok := findIBCDrip(snap.IBCDrips, req.Denom)
		if !ok {
			pourRequestsTotal.WithLabelValues(req.ChainID, "denom_not_found").Inc()
			writeError(w, http.StatusBadRequest, "no IBC drip configured for denom")
			return
		}
		cc, err := buildChainContextIBC(snap, ibcDrip)
		if err != nil {
			slog.ErrorContext(r.Context(), "pour: invalid IBC drip config", "chain_id", req.ChainID, "error", err)
			writeError(w, http.StatusInternalServerError, "invalid drip config")
			return
		}
		decision, err := h.gate.Admit(r.Context(), r, &req, cc)
		if err != nil {
			h.handleAdmitError(w, req.ChainID, err)
			return
		}
		h.pourIBC(w, r, snap, req.Address, decision, ibcDrip)
		return
	}

	if snap.Drip.Anonymous == "" {
		// IBC-only chain with no denom specified — check if exactly one IBC drip exists.
		if len(snap.IBCDrips) == 1 {
			ibcDrip := snap.IBCDrips[0]
			cc, err := buildChainContextIBC(snap, ibcDrip)
			if err != nil {
				slog.ErrorContext(r.Context(), "pour: invalid IBC drip config", "chain_id", req.ChainID, "error", err)
				writeError(w, http.StatusInternalServerError, "invalid drip config")
				return
			}
			decision, err := h.gate.Admit(r.Context(), r, &req, cc)
			if err != nil {
				h.handleAdmitError(w, req.ChainID, err)
				return
			}
			h.pourIBC(w, r, snap, req.Address, decision, ibcDrip)
			return
		}
		pourRequestsTotal.WithLabelValues(req.ChainID, "denom_required").Inc()
		writeError(w, http.StatusBadRequest, "chain has no native drip; specify denom for IBC drip")
		return
	}

	cc, err := buildChainContext(snap)
	if err != nil {
		slog.ErrorContext(r.Context(), "pour: invalid drip config", "chain_id", req.ChainID, "error", err)
		writeError(w, http.StatusInternalServerError, "invalid drip config")
		return
	}

	decision, err := h.gate.Admit(r.Context(), r, &req, cc)
	if err != nil {
		h.handleAdmitError(w, req.ChainID, err)
		return
	}

	dripCoin := decision.DripCoin
	mechanism := string(decision.Mechanism)
	coins := tx.Coins{{Amount: dripCoin.Amount, Denom: dripCoin.Denom}}
	amount := fmt.Sprintf("%s%s", dripCoin.Amount, dripCoin.Denom)
	ip := extractIP(r.RemoteAddr)
	now := time.Now().Unix()

	if h.pourer != nil {
		if handled := h.tryPoolRoute(w, r, req.ChainID, req.Address, coins, amount, ip, mechanism, now); handled {
			return
		}
	}

	h.pourSync(w, r, req.ChainID, req.Address, coins, amount, ip, mechanism, now)
}

// findIBCDrip returns the IBCDripConfig whose anonymous coin's denom matches denom.
func findIBCDrip(drips []config.IBCDripConfig, denom string) (config.IBCDripConfig, bool) {
	for _, d := range drips {
		coin, err := config.ParseCoin(d.Anonymous)
		if err == nil && coin.Denom == denom {
			return d, true
		}
	}
	return config.IBCDripConfig{}, false
}

// buildChainContext constructs ChainContext for the native drip path.
func buildChainContext(snap chain.ChainSnapshot) (abuse.ChainContext, error) {
	anonCoin, err := config.ParseCoin(snap.Drip.Anonymous)
	if err != nil {
		return abuse.ChainContext{}, fmt.Errorf("parse drip.anonymous: %w", err)
	}
	maxPerDay, err := config.ParseCoin(snap.Drip.MaxPerAddressPerDay)
	if err != nil {
		return abuse.ChainContext{}, fmt.Errorf("parse drip.max_per_address_per_day: %w", err)
	}
	var signedCoin tx.Coin
	if snap.Drip.Signed != "" {
		if signedCoin, err = config.ParseCoin(snap.Drip.Signed); err != nil {
			return abuse.ChainContext{}, fmt.Errorf("parse drip.signed: %w", err)
		}
	}
	return abuse.ChainContext{
		ChainID:       snap.Info.ChainID,
		KeyAlgo:       string(snap.Info.KeyAlgo),
		DripAnonymous: anonCoin,
		DripSigned:    signedCoin,
		MaxPerDay:     maxPerDay,
	}, nil
}

// buildChainContextIBC constructs ChainContext for an IBC drip path.
// Signed drip is not supported for IBC drips (no signed amount configured per-IBC-entry).
func buildChainContextIBC(snap chain.ChainSnapshot, ibcDrip config.IBCDripConfig) (abuse.ChainContext, error) {
	anonCoin, err := config.ParseCoin(ibcDrip.Anonymous)
	if err != nil {
		return abuse.ChainContext{}, fmt.Errorf("parse ibc drip anonymous: %w", err)
	}
	maxPerDay, err := config.ParseCoin(ibcDrip.MaxPerAddressPerDay)
	if err != nil {
		return abuse.ChainContext{}, fmt.Errorf("parse ibc drip max_per_address_per_day: %w", err)
	}
	return abuse.ChainContext{
		ChainID:       snap.Info.ChainID,
		KeyAlgo:       string(snap.Info.KeyAlgo),
		DripAnonymous: anonCoin,
		MaxPerDay:     maxPerDay,
	}, nil
}

func (*Handler) handleAdmitError(w http.ResponseWriter, chainID string, err error) {
	var rl *ratelimit.ErrRateLimitExceeded
	switch {
	case errors.As(err, &rl):
		pourRequestsTotal.WithLabelValues(chainID, "rate_limited").Inc()
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(rl.RetryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, abuse.ErrUnauthenticated):
		pourRequestsTotal.WithLabelValues(chainID, "unauthenticated").Inc()
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, abuse.ErrForbidden), errors.Is(err, abuse.ErrPredicateFailed):
		pourRequestsTotal.WithLabelValues(chainID, "forbidden").Inc()
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, abuse.ErrPoWRequired),
		errors.Is(err, abuse.ErrBadPoW),
		errors.Is(err, abuse.ErrNonceRequired),
		errors.Is(err, abuse.ErrBadNonce),
		errors.Is(err, abuse.ErrBadSignature):
		pourRequestsTotal.WithLabelValues(chainID, "bad_credential").Inc()
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("gate.Admit failed", "error", err, "chain_id", chainID)
		writeError(w, http.StatusInternalServerError, "admission check failed")
	}
}

// tryPoolRoute routes the request to the batch pool. Returns true when the request
// is fully handled (queued or error response sent), or false when ErrSyncMode is
// returned and the caller should fall through to the sync path.
func (h *Handler) tryPoolRoute(
	w http.ResponseWriter, r *http.Request,
	chainID, address string, coins tx.Coins, amount, ip, mechanism string, now int64,
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
			Mechanism:   mechanism,
			Status:      pourapi.StatusQueued,
			RequestedAt: now,
		})
		if recordErr != nil {
			slog.ErrorContext(r.Context(), "pour: record queued drip", "error", recordErr)
		}
		pourRequestsTotal.WithLabelValues(chainID, "queued").Inc()
		writeJSON(w, http.StatusAccepted, pourapi.PourResponse{
			DripID:    dripID,
			Status:    pourapi.StatusQueued,
			Amount:    amount,
			Mechanism: mechanism,
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
func (h *Handler) pourSync(
	w http.ResponseWriter,
	r *http.Request,
	chainID, address string,
	coins tx.Coins, amount, ip, mechanism string,
	now int64,
) {
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
		Mechanism:   mechanism,
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
		DripID:    dripID,
		Status:    pourapi.StatusConfirmed,
		Amount:    amount,
		Mechanism: mechanism,
		TxHash:    result.TxHash,
	})
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func (h *Handler) pourIBC(
	w http.ResponseWriter, r *http.Request,
	destSnap chain.ChainSnapshot, address string, decision *abuse.Decision,
	ibcDrip config.IBCDripConfig,
) {
	chainID := destSnap.Info.ChainID
	dripCoin := decision.DripCoin
	amount := fmt.Sprintf("%s%s", dripCoin.Amount, dripCoin.Denom)
	ip := extractIP(r.RemoteAddr)
	now := time.Now().Unix()

	srcSnap, ok := h.source.GetActive(ibcDrip.SourceChainID)
	if !ok {
		pourRequestsTotal.WithLabelValues(chainID, "ibc_no_source").Inc()
		writeError(w, http.StatusServiceUnavailable, "source chain not active")
		return
	}

	channels := h.source.ChannelsFor(srcSnap.Info.ChainName)
	ch, ok, ambiguous := chainregistry.SelectChannel(channels, srcSnap.Info.ChainName, destSnap.Info.ChainName)
	if !ok {
		pourRequestsTotal.WithLabelValues(chainID, "ibc_no_channel").Inc()
		writeError(w, http.StatusServiceUnavailable, "no IBC channel to destination chain")
		return
	}
	if ambiguous {
		slog.WarnContext(r.Context(), "pourIBC: multiple live channels, using first",
			"src", srcSnap.Info.ChainName, "dst", destSnap.Info.ChainName)
	}

	channelID, portID, _, _ := ch.ChannelFor(srcSnap.Info.ChainName)

	// post-v1: consider distributor fan-out for MsgTransfer if running multiple
	// Hermes instances in parallel; single key is fine while the relayer is the bottleneck.
	result, err := h.source.IBCTransfer(r.Context(), ibcDrip.SourceChainID, tx.TransferRequest{
		KeyIndex:         0,
		SourcePort:       portID,
		SourceChannel:    channelID,
		Token:            tx.Coin{Denom: dripCoin.Denom, Amount: dripCoin.Amount},
		ReceiverAddress:  address,
		TimeoutTimestamp: uint64(time.Now().Add(destSnap.IBCTimeout).UnixNano()),
	})
	if err != nil {
		pourRequestsTotal.WithLabelValues(chainID, "ibc_broadcast_error").Inc()
		slog.ErrorContext(r.Context(), "pourIBC: transfer", "chain", chainID, "error", err)
		writeError(w, http.StatusBadGateway, "IBC transfer failed")
		return
	}

	slog.DebugContext(r.Context(), "pourIBC: transfer confirmed",
		"chain", chainID, "tx_hash", result.TxHash, "packet_seq", result.PacketSequence)

	dripID, recordErr := h.dripStore.RecordDrip(r.Context(), store.DripRecord{
		ChainID:     chainID,
		Address:     address,
		Coins:       amount,
		RequesterIP: ip,
		Mechanism:   "ibc",
		TxHash:      result.TxHash,
		Status:      pourapi.StatusConfirmed,
		RequestedAt: now,
		CompletedAt: now,
	})
	if recordErr != nil {
		slog.ErrorContext(r.Context(), "pourIBC: record drip", "tx_hash", result.TxHash, "error", recordErr)
	}

	pourRequestsTotal.WithLabelValues(chainID, "confirmed").Inc()
	writeJSON(w, http.StatusOK, pourapi.PourResponse{
		DripID:    dripID,
		Status:    pourapi.StatusConfirmed,
		Amount:    amount,
		Mechanism: "ibc",
		TxHash:    result.TxHash,
	})
}
