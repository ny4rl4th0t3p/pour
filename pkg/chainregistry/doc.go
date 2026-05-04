// Package chainregistry resolves chain configuration from two sources —
// a periodically-fetched live registry snapshot and local operator overrides
// in chains.yml — into a single *ChainInfo per chain.
//
// The package is goroutine-free: it holds no background workers. The refresh
// loop lives in internal/chain, which calls UpdateLive when new data arrives.
//
// The package is YAML-agnostic: override parsing lives in the daemon, which
// converts chains.yml into a typed *OverrideSet and passes it to New.
package chainregistry
