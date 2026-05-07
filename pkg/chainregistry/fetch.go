package chainregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

	// Logger is used to log non-fatal IBC channel fetch errors. Defaults to
	// slog.Default() when nil.
	Logger *slog.Logger
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
// single chain fetch fails; the caller should treat this as fatal on startup
// and non-fatal on refresh. IBC channel fetch errors are non-fatal and are
// logged at Warn level.
func FetchLive(ctx context.Context, opts FetchOptions) (*Snapshot, error) {
	if opts.BaseURL == "" {
		opts.BaseURL = defaultRegistryBaseURL
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
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

	// Fetch IBC channel data for all pairs of configured chains. Non-fatal.
	ibcChannels, ibcErrs := FetchIBCChannels(ctx, opts.BaseURL, chainNamePairs(opts.ChainIDs), opts.HTTPClient)
	for _, err := range ibcErrs {
		opts.Logger.Warn("ibc channel fetch error (non-fatal)", "error", err)
	}

	return &Snapshot{Chains: chains, IBCChannels: ibcChannels}, nil
}

// FetchIBCChannels fetches _IBC/ channel files for each pair of chain names.
// 404 responses are silently skipped — they indicate no channel between that
// pair. Other errors are collected and returned alongside any successfully
// parsed channels.
func FetchIBCChannels(ctx context.Context, baseURL string, pairs [][2]string, httpClient *http.Client) ([]IBCChannel, []error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	type result struct {
		channels []IBCChannel
		err      error
	}

	ch := make(chan result, len(pairs))
	for _, pair := range pairs {
		go func(a, b string) {
			channels, err := fetchIBCPair(ctx, httpClient, baseURL, a, b)
			ch <- result{channels: channels, err: err}
		}(pair[0], pair[1])
	}

	var (
		all      []IBCChannel
		fetchErr []error
	)
	for range pairs {
		r := <-ch
		if r.err != nil {
			fetchErr = append(fetchErr, r.err)
			continue
		}
		all = append(all, r.channels...)
	}
	return all, fetchErr
}

// chainNamePairs returns all unique pairs of chain names derived from the
// given chain IDs. With n IDs there are n*(n-1)/2 pairs.
func chainNamePairs(chainIDs []string) [][2]string {
	names := make([]string, len(chainIDs))
	for i, id := range chainIDs {
		names[i] = ChainNameFromID(id)
	}
	var pairs [][2]string
	for i := range len(names) {
		for j := i + 1; j < len(names); j++ {
			pairs = append(pairs, [2]string{names[i], names[j]})
		}
	}
	return pairs
}

func fetchIBCPair(ctx context.Context, client *http.Client, baseURL, nameA, nameB string) ([]IBCChannel, error) {
	url := baseURL + "/_IBC/" + ibcPairFilename(nameA, nameB)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("chainregistry: build ibc request for %s-%s: %w", nameA, nameB, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chainregistry: fetch ibc %s-%s: %w", nameA, nameB, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no channel between this pair — not an error
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chainregistry: fetch ibc %s-%s: unexpected status %d", nameA, nameB, resp.StatusCode)
	}

	var raw rawIBCFile
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("chainregistry: decode ibc %s-%s: %w", nameA, nameB, err)
	}
	return parseIBCFile(raw), nil
}

func fetchChain(ctx context.Context, client *http.Client, baseURL, chainID string) (rawChainInfo, error) {
	url := baseURL + "/" + ChainNameFromID(chainID) + "/chain.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
