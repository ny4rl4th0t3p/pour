package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const watcherHTTPTimeout = 3 * time.Second

// watcherPollInterval is the delay between /status polls.
// Exposed as a var so tests can override it without sleeping.
var watcherPollInterval = 5 * time.Second

// StartDevnetWatcher launches a background goroutine that polls the Tendermint
// RPC /status endpoint. When the reported block height regresses (indicating a
// devnet chain reset), it flushes the gas cache and reconnects the tx client for
// chainID without operator intervention.
//
// Should only be called in --auto mode.
func (m *Manager) StartDevnetWatcher(ctx context.Context, chainID, rpcAddr string) {
	go m.watchLoop(ctx, chainID, rpcAddr)
}

func (m *Manager) watchLoop(ctx context.Context, chainID, rpcAddr string) {
	statusURL := rpcAddr + "/status"
	httpClient := &http.Client{Timeout: watcherHTTPTimeout}

	var prevHeight int64 = -1

	ticker := time.NewTicker(watcherPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h, err := fetchBlockHeight(ctx, httpClient, statusURL)
			if err != nil {
				m.log.WarnContext(ctx, "devnet: watcher: RPC poll failed",
					"chain_id", chainID, "error", err)
				continue
			}
			if prevHeight >= 0 && h < prevHeight {
				m.log.WarnContext(ctx, "devnet: chain reset detected — reconnecting",
					"chain_id", chainID, "prev_height", prevHeight, "new_height", h)
				m.resetChain(ctx, chainID)
			}
			prevHeight = h
		}
	}
}

// resetChain closes the existing tx.Client for chainID, opens a fresh connection,
// and flushes the gas cache. Called on block height regression.
func (m *Manager) resetChain(ctx context.Context, chainID string) {
	m.mu.Lock()
	c, ok := m.chains[chainID]
	if !ok {
		m.mu.Unlock()
		return
	}

	c.Close()

	fresh, err := newChain(c.info, c.drip, m.gasCache, m.mnemonicFn(), m.cfgFor(chainID), m.log)
	if err != nil {
		m.mu.Unlock()
		m.log.ErrorContext(ctx, "devnet: chain reset: failed to reconnect",
			"chain_id", chainID, "error", err)
		return
	}
	if m.startCtx != nil {
		fresh.Start(m.startCtx)
	}
	m.chains[chainID] = fresh
	m.mu.Unlock()

	if m.gasCache != nil {
		if err := m.gasCache.Reset(ctx, chainID); err != nil {
			m.log.ErrorContext(ctx, "devnet: chain reset: failed to flush gas cache",
				"chain_id", chainID, "error", err)
		}
	}

	m.log.InfoContext(ctx, "devnet: chain reconnected after reset", "chain_id", chainID)
}

// rpcStatusResponse is the minimal subset of the Tendermint /status response needed
// to read the latest block height.
type rpcStatusResponse struct {
	Result struct {
		SyncInfo struct {
			LatestBlockHeight string `json:"latest_block_height"`
		} `json:"sync_info"`
	} `json:"result"`
}

func fetchBlockHeight(ctx context.Context, client *http.Client, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	var status rpcStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return 0, fmt.Errorf("decode /status: %w", err)
	}

	h, err := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse block height %q: %w", status.Result.SyncInfo.LatestBlockHeight, err)
	}
	return h, nil
}
