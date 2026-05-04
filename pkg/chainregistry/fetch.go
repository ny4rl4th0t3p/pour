package chainregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

const defaultRegistryBaseURL = "https://raw.githubusercontent.com/cosmos/chain-registry/master"

// FetchOptions configures a FetchLive call.
type FetchOptions struct {
	// BaseURL is the registry root; each chain is fetched from
	// <BaseURL>/<chain_name>/chain.json.
	// Defaults to the cosmos/chain-registry raw GitHub URL.
	BaseURL string

	// ChainIDs is the list of chain IDs to fetch. chain_name is derived from
	// each ID via ChainNameFromID.
	ChainIDs []string

	// HTTPClient is used for all requests. Defaults to http.DefaultClient.
	HTTPClient *http.Client

	// Timeout bounds the entire fetch (all chains). Zero means no added timeout
	// beyond any deadline already on ctx.
	Timeout time.Duration
}

var trailingNumericSuffix = regexp.MustCompile(`-\d+$`)

// ChainNameFromID derives the registry chain_name from a chain_id by stripping
// the trailing numeric suffix (e.g. "osmosis-1" → "osmosis", "cosmoshub-4" → "cosmoshub").
// If the ID has no numeric suffix it is returned as-is.
func ChainNameFromID(chainID string) string {
	return trailingNumericSuffix.ReplaceAllString(chainID, "")
}

// FetchLive fetches chain.json from the registry for each chain ID in
// opts.ChainIDs, running all requests concurrently. Returns an error if any
// single fetch fails; the caller should treat this as fatal on startup and
// non-fatal on refresh.
func FetchLive(ctx context.Context, opts FetchOptions) (*Snapshot, error) {
	if opts.BaseURL == "" {
		opts.BaseURL = defaultRegistryBaseURL
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	type result struct {
		chainID string
		raw     rawChainInfo
		err     error
	}

	ch := make(chan result, len(opts.ChainIDs))
	for _, chainID := range opts.ChainIDs {
		go func(id string) {
			raw, err := fetchChain(ctx, opts.HTTPClient, opts.BaseURL, id)
			ch <- result{chainID: id, raw: raw, err: err}
		}(chainID)
	}

	chains := make(map[string]rawChainInfo, len(opts.ChainIDs))
	var errs []error
	for range opts.ChainIDs {
		r := <-ch
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		chains[r.chainID] = r.raw
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &Snapshot{Chains: chains}, nil
}

func fetchChain(ctx context.Context, client *http.Client, baseURL, chainID string) (rawChainInfo, error) {
	url := baseURL + "/" + ChainNameFromID(chainID) + "/chain.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return rawChainInfo{}, fmt.Errorf("chainregistry: build request for %s: %w", chainID, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return rawChainInfo{}, fmt.Errorf("chainregistry: fetch %s: %w", chainID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return rawChainInfo{}, fmt.Errorf("chainregistry: fetch %s: unexpected status %d", chainID, resp.StatusCode)
	}
	var raw rawChainInfo
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return rawChainInfo{}, fmt.Errorf("chainregistry: decode %s: %w", chainID, err)
	}
	return raw, nil
}
