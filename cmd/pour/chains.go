package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

// ChainsCmd groups chain management subcommands.
type ChainsCmd struct {
	List     ChainsListCmd     `cmd:"" help:"List all chains in the registry snapshot."`
	Validate ChainsValidateCmd `cmd:"" help:"Validate a chains.yml file offline."`
	Diff     ChainsDiffCmd     `cmd:"" help:"Show what a config reload would change (requires daemon)."`
	Pending  ChainsPendingCmd  `cmd:"" help:"List pending freeze-policy changes (requires daemon)."`
	Accept   ChainsAcceptCmd   `cmd:"" help:"Accept pending freeze-policy changes (requires daemon)."`
	Pin      ChainsPinCmd      `cmd:"" help:"Print YAML snippet to pin a chain field (requires daemon)."`
	Refresh  ChainsRefreshCmd  `cmd:"" help:"Trigger an immediate registry refresh (requires daemon)."`
}

// --- response types (mirror Go field names — chainregistry types have no json tags) ---

type chainEndpoint struct {
	URL      string `json:"URL"`
	Provider string `json:"Provider"`
}

type chainFeeToken struct {
	Denom           string `json:"Denom"`
	LowGasPrice     string `json:"LowGasPrice"`
	AverageGasPrice string `json:"AverageGasPrice"`
	HighGasPrice    string `json:"HighGasPrice"`
	Display         string `json:"Display"`
	Exponent        uint32 `json:"Exponent"`
}

type chainEndpoints struct {
	GRPC []chainEndpoint `json:"GRPC"`
	RPC  []chainEndpoint `json:"RPC"`
	REST []chainEndpoint `json:"REST"`
}

type chainSources struct {
	Identity  int `json:"Identity"`
	Address   int `json:"Address"`
	Endpoints int `json:"Endpoints"`
	FeeTokens int `json:"FeeTokens"`
	BlockTime int `json:"BlockTime"`
}

type chainSnapshot struct {
	ChainID      string          `json:"ChainID"`
	ChainName    string          `json:"ChainName"`
	NetworkType  string          `json:"NetworkType"`
	PrettyName   string          `json:"PrettyName"`
	Bech32Prefix string          `json:"Bech32Prefix"`
	Slip44       uint32          `json:"Slip44"`
	KeyAlgo      string          `json:"KeyAlgo"`
	Endpoints    chainEndpoints  `json:"Endpoints"`
	FeeTokens    []chainFeeToken `json:"FeeTokens"`
	BlockTime    int64           `json:"BlockTime"` // time.Duration as nanoseconds
	Enabled      bool            `json:"Enabled"`
	LastChanged  time.Time       `json:"LastChanged"`
	Sources      chainSources    `json:"Sources"`
}

type pendingChange struct {
	ChainID    string    `json:"ChainID"`
	Field      string    `json:"Field"`
	OldValue   any       `json:"OldValue"`
	NewValue   any       `json:"NewValue"`
	DetectedAt time.Time `json:"DetectedAt"`
}

type refreshResult struct {
	HotReloaded int `json:"hot_reloaded"`
	Warned      int `json:"warned"`
	Frozen      int `json:"frozen"`
	Removed     int `json:"removed"`
}

// --- list ---

// ChainsListCmd prints all chains in the daemon's current registry snapshot.
type ChainsListCmd struct{}

func (*ChainsListCmd) Run() error {
	client, err := newAdminClient()
	if err != nil {
		return err
	}

	var chains []chainSnapshot
	if err := client.getJSON("/admin/registry/snapshot", &chains); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CHAIN_ID\tNAME\tTYPE\tBECH32\tENABLED\tLAST_CHANGED")
	for i := range chains {
		c := &chains[i]
		lc := ""
		if !c.LastChanged.IsZero() {
			lc = c.LastChanged.Format(time.RFC3339)
		}
		enabled := "no"
		if c.Enabled {
			enabled = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			c.ChainID, c.ChainName, c.NetworkType, c.Bech32Prefix, enabled, lc)
	}
	return w.Flush()
}

// --- validate ---

// ChainsValidateCmd validates a chains.yml file offline.
type ChainsValidateCmd struct {
	Config string `arg:"" type:"path" help:"Path to chains.yml."`
}

func (c *ChainsValidateCmd) Run() error {
	cfg, err := config.LoadChains(c.Config)
	if err != nil {
		return err
	}
	if _, err := cfg.ToOverrideSet(); err != nil {
		return fmt.Errorf("override set: %w", err)
	}
	if _, err := cfg.ToStandaloneInfos(); err != nil {
		return fmt.Errorf("standalone chains: %w", err)
	}
	fmt.Println("OK")
	return nil
}

// --- diff ---

// ChainsDiffCmd shows what a config reload would change.
type ChainsDiffCmd struct {
	Config string `short:"c" default:"chains.yml" help:"Path to chains.yml."`
}

func (c *ChainsDiffCmd) Run() error {
	client, err := newAdminClient()
	if err != nil {
		return err
	}

	var snaps []chainSnapshot
	if err := client.getJSON("/admin/registry/snapshot", &snaps); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	byID := make(map[string]*chainSnapshot, len(snaps))
	for i := range snaps {
		byID[snaps[i].ChainID] = &snaps[i]
	}

	cfg, err := config.LoadChains(c.Config)
	if err != nil {
		return err
	}

	anyChange := false
	for i := range cfg.Chains {
		cc := &cfg.Chains[i]
		snap, inSnap := byID[cc.ChainID]
		if !inSnap {
			fmt.Printf("%s: not in daemon snapshot (new chain requires restart)\n", cc.ChainID)
			anyChange = true
			continue
		}
		lines := diffChainLines(cc, snap)
		if len(lines) == 0 {
			continue
		}
		fmt.Printf("%s:\n", cc.ChainID)
		for _, l := range lines {
			fmt.Println(l)
		}
		anyChange = true
	}

	if !anyChange {
		fmt.Println("no changes")
	}
	return nil
}

func diffChainLines(cc *config.ChainConfig, snap *chainSnapshot) []string {
	var lines []string
	if cc.IsEnabled() != snap.Enabled {
		lines = append(lines, fmt.Sprintf("  enabled: %v -> %v", snap.Enabled, cc.IsEnabled()))
	}
	lines = append(lines, diffIdentityLines(cc, snap)...)
	lines = append(lines, diffEndpointLines(cc, snap)...)
	lines = append(lines, diffFeeTokenLines(cc, snap)...)
	return lines
}

func diffIdentityLines(cc *config.ChainConfig, snap *chainSnapshot) []string {
	var lines []string
	if cc.ChainName != nil && *cc.ChainName != snap.ChainName {
		lines = append(lines, fmt.Sprintf("  chain_name: %q -> %q", snap.ChainName, *cc.ChainName))
	}
	if cc.NetworkType != nil && *cc.NetworkType != snap.NetworkType {
		lines = append(lines, fmt.Sprintf("  network_type: %q -> %q", snap.NetworkType, *cc.NetworkType))
	}
	if cc.KeyAlgo != nil && *cc.KeyAlgo != snap.KeyAlgo {
		lines = append(lines, fmt.Sprintf("  key_algo: %q -> %q", snap.KeyAlgo, *cc.KeyAlgo))
	}
	if cc.Bech32Prefix != nil && *cc.Bech32Prefix != snap.Bech32Prefix {
		lines = append(lines, fmt.Sprintf("  bech32_prefix: %q -> %q", snap.Bech32Prefix, *cc.Bech32Prefix))
	}
	if cc.Slip44 != nil && *cc.Slip44 != snap.Slip44 {
		lines = append(lines, fmt.Sprintf("  slip44: %d -> %d", snap.Slip44, *cc.Slip44))
	}
	if cc.BlockTime != nil {
		d, _ := time.ParseDuration(*cc.BlockTime)
		if int64(d) != snap.BlockTime {
			lines = append(lines, fmt.Sprintf("  block_time: %s -> %s", time.Duration(snap.BlockTime), d))
		}
	}
	return lines
}

func diffEndpointLines(cc *config.ChainConfig, snap *chainSnapshot) []string {
	if cc.Endpoints == nil {
		return nil
	}
	var lines []string
	if cc.Endpoints.GRPC != nil {
		cur := endpointURLs(snap.Endpoints.GRPC)
		if !stringSliceEqual(cur, cc.Endpoints.GRPC) {
			lines = append(lines, fmt.Sprintf("  endpoints.grpc: [%s] -> [%s]",
				strings.Join(cur, ", "), strings.Join(cc.Endpoints.GRPC, ", ")))
		}
	}
	if cc.Endpoints.RPC != nil {
		cur := endpointURLs(snap.Endpoints.RPC)
		if !stringSliceEqual(cur, cc.Endpoints.RPC) {
			lines = append(lines, fmt.Sprintf("  endpoints.rpc: [%s] -> [%s]",
				strings.Join(cur, ", "), strings.Join(cc.Endpoints.RPC, ", ")))
		}
	}
	if cc.Endpoints.REST != nil {
		cur := endpointURLs(snap.Endpoints.REST)
		if !stringSliceEqual(cur, cc.Endpoints.REST) {
			lines = append(lines, fmt.Sprintf("  endpoints.rest: [%s] -> [%s]",
				strings.Join(cur, ", "), strings.Join(cc.Endpoints.REST, ", ")))
		}
	}
	return lines
}

func diffFeeTokenLines(cc *config.ChainConfig, snap *chainSnapshot) []string {
	var lines []string
	for _, ft := range cc.FeeTokens {
		snapFT := snapFeeToken(snap.FeeTokens, ft.Denom)
		if snapFT == nil {
			continue
		}
		if ft.LowGasPrice != nil && *ft.LowGasPrice != snapFT.LowGasPrice {
			lines = append(lines, fmt.Sprintf("  fee_tokens[%s].low_gas_price: %s -> %s",
				ft.Denom, snapFT.LowGasPrice, *ft.LowGasPrice))
		}
		if ft.AverageGasPrice != nil && *ft.AverageGasPrice != snapFT.AverageGasPrice {
			lines = append(lines, fmt.Sprintf("  fee_tokens[%s].average_gas_price: %s -> %s",
				ft.Denom, snapFT.AverageGasPrice, *ft.AverageGasPrice))
		}
		if ft.HighGasPrice != nil && *ft.HighGasPrice != snapFT.HighGasPrice {
			lines = append(lines, fmt.Sprintf("  fee_tokens[%s].high_gas_price: %s -> %s",
				ft.Denom, snapFT.HighGasPrice, *ft.HighGasPrice))
		}
	}
	return lines
}

// --- pending ---

// ChainsPendingCmd lists pending freeze-policy changes.
type ChainsPendingCmd struct{}

func (*ChainsPendingCmd) Run() error {
	client, err := newAdminClient()
	if err != nil {
		return err
	}

	var pending []pendingChange
	if err := client.getJSON("/admin/registry/pending", &pending); err != nil {
		return fmt.Errorf("pending: %w", err)
	}

	if len(pending) == 0 {
		fmt.Println("no pending changes")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CHAIN_ID\tFIELD\tOLD_VALUE\tNEW_VALUE\tDETECTED_AT")
	for _, p := range pending {
		fmt.Fprintf(w, "%s\t%s\t%v\t%v\t%s\n",
			p.ChainID, p.Field,
			formatAny(p.OldValue), formatAny(p.NewValue),
			p.DetectedAt.Format(time.RFC3339))
	}
	return w.Flush()
}

// --- accept ---

// ChainsAcceptCmd accepts pending freeze-policy changes.
type ChainsAcceptCmd struct {
	All   bool   `short:"a" help:"Accept all pending changes across all chains."`
	Chain string `arg:"" optional:"" help:"Chain ID."`
	Field string `arg:"" optional:"" help:"Field name (omit for all fields on this chain)."`
}

func (c *ChainsAcceptCmd) Run() error {
	if c.All && c.Chain != "" {
		return fmt.Errorf("cannot use --all with a chain ID argument")
	}
	if !c.All && c.Chain == "" {
		return fmt.Errorf("chain ID required (or use --all)")
	}

	client, err := newAdminClient()
	if err != nil {
		return err
	}

	if c.All {
		return acceptAll(client)
	}

	body := map[string]string{"chain_id": c.Chain}
	if c.Field != "" {
		body["field"] = c.Field
	}
	var result map[string]bool
	if err := client.postJSON("/admin/registry/accept", body, &result); err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	if c.Field != "" {
		fmt.Printf("accepted %s/%s\n", c.Chain, c.Field)
	} else {
		fmt.Printf("accepted all pending changes for %s\n", c.Chain)
	}
	return nil
}

func acceptAll(client *adminClient) error {
	var pending []pendingChange
	if err := client.getJSON("/admin/registry/pending", &pending); err != nil {
		return fmt.Errorf("pending: %w", err)
	}
	if len(pending) == 0 {
		fmt.Println("no pending changes")
		return nil
	}

	// Collect unique chain IDs to use the server's "accept all for chain" path.
	seen := make(map[string]struct{})
	var chainIDs []string
	for _, p := range pending {
		if _, ok := seen[p.ChainID]; !ok {
			seen[p.ChainID] = struct{}{}
			chainIDs = append(chainIDs, p.ChainID)
		}
	}

	accepted := 0
	for _, id := range chainIDs {
		body := map[string]string{"chain_id": id}
		var result map[string]bool
		if err := client.postJSON("/admin/registry/accept", body, &result); err != nil {
			return fmt.Errorf("accept %s: %w", id, err)
		}
		for _, p := range pending {
			if p.ChainID == id {
				accepted++
			}
		}
	}
	fmt.Printf("accepted %d change(s) across %d chain(s)\n", accepted, len(chainIDs))
	return nil
}

// --- pin ---

// ChainsPinCmd prints a YAML snippet for pinning a chain field to its current value.
type ChainsPinCmd struct {
	Chain string `arg:"" help:"Chain ID."`
	Field string `arg:"" help:"Field name (e.g. Bech32Prefix, Slip44, Endpoints.GRPC)."`
}

func (c *ChainsPinCmd) Run() error {
	client, err := newAdminClient()
	if err != nil {
		return err
	}

	var snaps []chainSnapshot
	if err := client.getJSON("/admin/registry/snapshot", &snaps); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	var snap *chainSnapshot
	for i := range snaps {
		if snaps[i].ChainID == c.Chain {
			snap = &snaps[i]
			break
		}
	}
	if snap == nil {
		return fmt.Errorf("chain %q not found in snapshot", c.Chain)
	}

	snippet, err := pinSnippet(snap, c.Field)
	if err != nil {
		return err
	}
	fmt.Printf("# Add under the entry for chain_id: %s in chains.yml:\n", c.Chain)
	fmt.Print(snippet)
	return nil
}

// pinSnippet returns a YAML snippet for the requested field.
func pinSnippet(snap *chainSnapshot, field string) (string, error) {
	switch strings.ToLower(field) {
	case "chainname", "chain_name":
		return fmt.Sprintf("chain_name: %q\n", snap.ChainName), nil
	case "networktype", "network_type":
		return fmt.Sprintf("network_type: %q\n", snap.NetworkType), nil
	case "keyalgo", "key_algo":
		return fmt.Sprintf("key_algo: %q\n", snap.KeyAlgo), nil
	case "bech32prefix", "bech32_prefix":
		return fmt.Sprintf("bech32_prefix: %q\n", snap.Bech32Prefix), nil
	case "slip44":
		return fmt.Sprintf("slip44: %d\n", snap.Slip44), nil
	case "blocktime", "block_time":
		d := time.Duration(snap.BlockTime)
		return fmt.Sprintf("block_time: %q\n", d.String()), nil
	case "endpoints.grpc":
		return endpointsSnippet("grpc", snap.Endpoints.GRPC), nil
	case "endpoints.rpc":
		return endpointsSnippet("rpc", snap.Endpoints.RPC), nil
	case "endpoints.rest":
		return endpointsSnippet("rest", snap.Endpoints.REST), nil
	case "feetokens.lowgasprice", "fee_tokens.low_gas_price":
		return feeTokenPriceSnippet(snap.FeeTokens, "low_gas_price"), nil
	case "feetokens.averagegasprice", "fee_tokens.average_gas_price":
		return feeTokenPriceSnippet(snap.FeeTokens, "average_gas_price"), nil
	case "feetokens.highgasprice", "fee_tokens.high_gas_price":
		return feeTokenPriceSnippet(snap.FeeTokens, "high_gas_price"), nil
	default:
		return "", fmt.Errorf(
			"unknown field %q; supported: ChainName, NetworkType, KeyAlgo, Bech32Prefix, "+
				"Slip44, BlockTime, Endpoints.GRPC, Endpoints.RPC, Endpoints.REST, "+
				"FeeTokens.LowGasPrice, FeeTokens.AverageGasPrice, FeeTokens.HighGasPrice",
			field,
		)
	}
}

func endpointsSnippet(proto string, eps []chainEndpoint) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "endpoints:\n  %s:\n", proto)
	for _, ep := range eps {
		fmt.Fprintf(&sb, "    - %s\n", ep.URL)
	}
	return sb.String()
}

func feeTokenPriceSnippet(fts []chainFeeToken, priceField string) string {
	if len(fts) == 0 {
		return "fee_tokens: []\n"
	}
	var sb strings.Builder
	fmt.Fprintln(&sb, "fee_tokens:")
	for _, ft := range fts {
		var price string
		switch priceField {
		case "low_gas_price":
			price = ft.LowGasPrice
		case "average_gas_price":
			price = ft.AverageGasPrice
		case "high_gas_price":
			price = ft.HighGasPrice
		}
		fmt.Fprintf(&sb, "  - denom: %s\n    %s: %q\n", ft.Denom, priceField, price)
	}
	return sb.String()
}

// --- refresh ---

// ChainsRefreshCmd triggers an immediate live registry refresh.
type ChainsRefreshCmd struct{}

func (*ChainsRefreshCmd) Run() error {
	client, err := newAdminClient()
	if err != nil {
		return err
	}

	var result refreshResult
	if err := client.postJSON("/admin/registry/refresh", nil, &result); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}

	fmt.Printf("hot-reloaded: %d  warned: %d  frozen: %d  removed: %d\n",
		result.HotReloaded, result.Warned, result.Frozen, result.Removed)
	return nil
}

// --- helpers ---

func endpointURLs(eps []chainEndpoint) []string {
	urls := make([]string, len(eps))
	for i, ep := range eps {
		urls[i] = ep.URL
	}
	return urls
}

func snapFeeToken(fts []chainFeeToken, denom string) *chainFeeToken {
	for i := range fts {
		if fts[i].Denom == denom {
			return &fts[i]
		}
	}
	return nil
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func formatAny(v any) string {
	if v == nil {
		return "<nil>"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	s := string(b)
	// Trim quotes from simple strings for readability.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
