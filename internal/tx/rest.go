package tx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// restTransport implements transport over the Cosmos REST (LCD) API.
type restTransport struct {
	baseURL    string
	httpClient *http.Client
}

func newRESTTransport(baseURL string) *restTransport {
	return &restTransport{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (r *restTransport) endpointURL() string { return r.baseURL }
func (*restTransport) close() error          { return nil }

// queryAccount fetches account number and sequence via GET /cosmos/auth/v1beta1/accounts/{address}.
func (r *restTransport) queryAccount(ctx context.Context, address string) (*Account, error) {
	url := r.baseURL + "/cosmos/auth/v1beta1/accounts/" + address
	body, statusCode, err := r.get(ctx, url)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusNotFound {
		return nil, ErrAccountNotFound
	}
	if statusCode >= http.StatusInternalServerError {
		return nil, fmt.Errorf("%w: GET %s: status %d", errRESTUnavailable, url, statusCode)
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("tx: query account %s: status %d: %s", address, statusCode, body)
	}

	var resp struct {
		Account struct {
			Type    string `json:"@type"`
			Address string `json:"address"`
			// BaseAccount fields (direct)
			AccountNumber string `json:"account_number"`
			Sequence      string `json:"sequence"`
			// EthAccount wraps BaseAccount
			BaseAccount *struct {
				Address       string `json:"address"`
				AccountNumber string `json:"account_number"`
				Sequence      string `json:"sequence"`
			} `json:"base_account"`
		} `json:"account"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tx: query account %s: decode: %w", address, err)
	}

	acc := &Account{Address: address}
	src := resp.Account

	if src.BaseAccount != nil {
		// EthAccount or similar wrapper.
		acc.Address = src.BaseAccount.Address
		acc.AccountNumber, _ = strconv.ParseUint(src.BaseAccount.AccountNumber, 10, 64)
		acc.Sequence, _ = strconv.ParseUint(src.BaseAccount.Sequence, 10, 64)
	} else {
		acc.AccountNumber, _ = strconv.ParseUint(src.AccountNumber, 10, 64)
		acc.Sequence, _ = strconv.ParseUint(src.Sequence, 10, 64)
	}
	if acc.Address == "" {
		acc.Address = address
	}
	return acc, nil
}

// simulate calls POST /cosmos/tx/v1beta1/simulate. Returns (0, nil) when
// simulation is unavailable — the caller falls through to the next fee strategy.
func (r *restTransport) simulate(ctx context.Context, txBytes []byte) (uint64, error) {
	payload, _ := json.Marshal(map[string]string{
		"tx_bytes": base64.StdEncoding.EncodeToString(txBytes),
	})
	body, statusCode, err := r.post(ctx, r.baseURL+"/cosmos/tx/v1beta1/simulate", payload)
	if err != nil || statusCode != http.StatusOK {
		return 0, nil //nolint:nilerr // simulation is optional
	}
	var resp struct {
		GasInfo struct {
			GasUsed string `json:"gas_used"`
		} `json:"gas_info"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.GasInfo.GasUsed == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(resp.GasInfo.GasUsed, 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

// broadcastTx calls POST /cosmos/tx/v1beta1/txs with BROADCAST_MODE_SYNC.
func (r *restTransport) broadcastTx(ctx context.Context, txBytes []byte) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"tx_bytes": base64.StdEncoding.EncodeToString(txBytes),
		"mode":     "BROADCAST_MODE_SYNC",
	})
	url := r.baseURL + "/cosmos/tx/v1beta1/txs"
	body, statusCode, err := r.post(ctx, url, payload)
	if err != nil {
		return "", err
	}
	if statusCode >= http.StatusInternalServerError {
		return "", fmt.Errorf("%w: POST %s: status %d", errRESTUnavailable, url, statusCode)
	}

	// Some chains surface sequence/fee errors at the HTTP level (4xx) with a code field.
	if statusCode != http.StatusOK {
		var errResp struct {
			Code    uint32 `json:"code"`
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Code != 0 {
			return "", classifyCodeAndLog(errResp.Code, errResp.Message)
		}
		return "", fmt.Errorf("tx: broadcast: status %d: %s", statusCode, body)
	}

	var resp struct {
		TxResponse struct {
			Code   uint32 `json:"code"`
			Txhash string `json:"txhash"`
			RawLog string `json:"raw_log"`
		} `json:"tx_response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("tx: broadcast: decode: %w", err)
	}
	if resp.TxResponse.Code != 0 {
		return "", classifyCodeAndLog(resp.TxResponse.Code, resp.TxResponse.RawLog)
	}
	return resp.TxResponse.Txhash, nil
}

// waitForConfirmation polls GET /cosmos/tx/v1beta1/txs/{hash} until the tx is
// included in a block or the deadline is reached.
func (r *restTransport) waitForConfirmation(ctx context.Context, txHash string) (*BroadcastResult, error) {
	url := r.baseURL + "/cosmos/tx/v1beta1/txs/" + txHash
	deadline := time.Now().Add(confirmTimeout)
	for {
		if time.Now().After(deadline) {
			return nil, ErrBroadcastTimeout
		}

		body, statusCode, err := r.get(ctx, url)
		if err != nil || statusCode == http.StatusNotFound {
			if !sleepOrCancel(ctx, confirmPollInterval) {
				return nil, ctx.Err()
			}
			continue
		}
		if statusCode >= http.StatusInternalServerError {
			// Transient; retry.
			if !sleepOrCancel(ctx, confirmPollInterval) {
				return nil, ctx.Err()
			}
			continue
		}
		if statusCode != http.StatusOK {
			return nil, fmt.Errorf("tx: confirmation: status %d: %s", statusCode, body)
		}

		var resp struct {
			TxResponse struct {
				Code    uint32 `json:"code"`
				Txhash  string `json:"txhash"`
				Height  string `json:"height"`
				GasUsed string `json:"gas_used"`
				RawLog  string `json:"raw_log"`
			} `json:"tx_response"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("tx: confirmation: decode: %w", err)
		}
		if resp.TxResponse.Code != 0 {
			return nil, classifyCodeAndLog(resp.TxResponse.Code, resp.TxResponse.RawLog)
		}
		height, _ := strconv.ParseInt(resp.TxResponse.Height, 10, 64)
		gasUsed, _ := strconv.ParseUint(resp.TxResponse.GasUsed, 10, 64)
		return &BroadcastResult{
			TxHash:  resp.TxResponse.Txhash,
			Height:  height,
			GasUsed: gasUsed,
		}, nil
	}
}

// queryBalance calls GET /cosmos/bank/v1beta1/balances/{address}/by_denom.
func (r *restTransport) queryBalance(ctx context.Context, address, denom string) (Coin, error) {
	url := r.baseURL + "/cosmos/bank/v1beta1/balances/" + address + "/by_denom?denom=" + denom
	body, statusCode, err := r.get(ctx, url)
	if err != nil {
		return Coin{}, err
	}
	if statusCode >= http.StatusInternalServerError {
		return Coin{}, fmt.Errorf("%w: GET %s: status %d", errRESTUnavailable, url, statusCode)
	}
	if statusCode == http.StatusNotFound {
		return Coin{Denom: denom, Amount: "0"}, nil
	}
	if statusCode != http.StatusOK {
		return Coin{}, fmt.Errorf("tx: query balance %s/%s: status %d", address, denom, statusCode)
	}
	var resp struct {
		Balance struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"balance"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Coin{}, fmt.Errorf("tx: query balance %s/%s: decode: %w", address, denom, err)
	}
	if resp.Balance.Amount == "" {
		return Coin{Denom: denom, Amount: "0"}, nil
	}
	return Coin{Denom: resp.Balance.Denom, Amount: resp.Balance.Amount}, nil
}

func (r *restTransport) get(ctx context.Context, url string) (body []byte, statusCode int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: build request: %w", errRESTUnavailable, err)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", errRESTUnavailable, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%w: read body: %w", errRESTUnavailable, err)
	}
	return body, resp.StatusCode, nil
}

func (r *restTransport) post(ctx context.Context, url string, payload []byte) (body []byte, statusCode int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: build request: %w", errRESTUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", errRESTUnavailable, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%w: read body: %w", errRESTUnavailable, err)
	}
	return body, resp.StatusCode, nil
}
