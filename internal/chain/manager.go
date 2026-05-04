package chain

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ny4rl4th0t3p/pour/internal/config"
	"github.com/ny4rl4th0t3p/pour/internal/gascache"
	"github.com/ny4rl4th0t3p/pour/pkg/chainregistry"
)

const defaultRefreshInterval = 6 * time.Hour

// Options configures a new Manager.
type Options struct {
	Config          *config.ChainsConfig
	GasCache        *gascache.Cache
	Logger          *slog.Logger
	RegistryBaseURL string        // empty → cosmos/chain-registry GitHub URL
	RefreshInterval time.Duration // 0 → 6h
}

// Manager owns the lifecycle of all active chain connections and the registry store.
// It is goroutine-safe.
type Manager struct {
	regStore        *chainregistry.Store
	gasCache        *gascache.Cache
	drips           map[string]chainregistry.DripPolicy // all chains, both standalone and registry
	registryIDs     []string
	refreshInterval time.Duration
	registryBaseURL string

	mu     sync.RWMutex
	chains map[string]*Chain

	log *slog.Logger
}

// New creates a Manager: fetches live data for all enabled registry chains, adds
// standalone chains, and opens tx.Client connections for all enabled chains.
// Returns an error if the initial registry fetch fails.
func New(ctx context.Context, opts Options) (*Manager, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = defaultRefreshInterval
	}

	ov, err := opts.Config.ToOverrideSet()
	if err != nil {
		return nil, err
	}

	regStore, err := chainregistry.New(chainregistry.Options{
		Overrides: ov,
		Logger:    opts.Logger,
	})
	if err != nil {
		return nil, err
	}

	standalones, err := opts.Config.ToStandaloneInfos()
	if err != nil {
		return nil, err
	}
	if len(standalones) > 0 {
		regStore.AddStandalone(standalones...)
	}

	registryIDs := opts.Config.EnabledRegistryChainIDs()
	if len(registryIDs) > 0 {
		snap, err := chainregistry.FetchLive(ctx, chainregistry.FetchOptions{
			BaseURL:  opts.RegistryBaseURL,
			ChainIDs: registryIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("chain: initial registry fetch: %w", err)
		}
		if _, err := regStore.UpdateLive(snap); err != nil {
			return nil, fmt.Errorf("chain: populate store: %w", err)
		}
	}

	m := &Manager{
		regStore:        regStore,
		gasCache:        opts.GasCache,
		drips:           buildDripMap(opts.Config),
		registryIDs:     registryIDs,
		refreshInterval: opts.RefreshInterval,
		registryBaseURL: opts.RegistryBaseURL,
		chains:          make(map[string]*Chain),
		log:             opts.Logger,
	}

	if err := m.reconcile(); err != nil {
		m.closeAll()
		return nil, err
	}

	return m, nil
}

// Get returns the active Chain for chainID.
// Returns an error if the chain is not enabled or not found.
func (m *Manager) Get(chainID string) (*Chain, error) {
	m.mu.RLock()
	c := m.chains[chainID]
	m.mu.RUnlock()
	if c == nil {
		return nil, fmt.Errorf("chain: %q: not found or not enabled", chainID)
	}
	return c, nil
}

// ListActive returns all active chains, sorted by chain ID.
func (m *Manager) ListActive() []*Chain {
	m.mu.RLock()
	out := make([]*Chain, 0, len(m.chains))
	for _, c := range m.chains {
		out = append(out, c)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].info.ChainID < out[j].info.ChainID
	})
	return out
}

// StartRefreshLoop launches a background goroutine that periodically re-fetches
// registry data. It stops when ctx is canceled. No-op if there are no registry chains.
func (m *Manager) StartRefreshLoop(ctx context.Context) {
	if len(m.registryIDs) == 0 {
		return
	}
	go m.refreshLoop(ctx)
}

// Close closes all active chain connections.
func (m *Manager) Close() {
	m.closeAll()
}

var refreshBackoff = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	time.Hour,
}

func (m *Manager) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()

	backoffIdx := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		snap, err := chainregistry.FetchLive(ctx, chainregistry.FetchOptions{
			BaseURL:  m.registryBaseURL,
			ChainIDs: m.registryIDs,
		})
		if err != nil {
			m.log.Error("chain: registry refresh failed", "err", err)
			delay := refreshBackoff[min(backoffIdx, len(refreshBackoff)-1)]
			backoffIdx++
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			continue
		}

		backoffIdx = 0
		if _, err := m.regStore.UpdateLive(snap); err != nil {
			m.log.Error("chain: registry update failed", "err", err)
			continue
		}
		if err := m.reconcile(); err != nil {
			m.log.Error("chain: reconcile after refresh failed", "err", err)
		}
	}
}

// reconcile opens connections for newly-enabled chains and closes connections
// for chains that are no longer enabled.
func (m *Manager) reconcile() error {
	all := m.regStore.List()

	enabled := make(map[string]*chainregistry.ChainInfo, len(all))
	for _, info := range all {
		if info.Enabled {
			enabled[info.ChainID] = info
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, c := range m.chains {
		if _, ok := enabled[id]; !ok {
			c.Close()
		}
	}

	active := make(map[string]*Chain, len(enabled))
	for id, info := range enabled {
		if c, ok := m.chains[id]; ok {
			active[id] = c
			continue
		}
		drip := m.dripFor(id)
		c, err := newChain(info, drip, m.gasCache)
		if err != nil {
			for _, ac := range active {
				ac.Close()
			}
			return fmt.Errorf("chain %q: %w", id, err)
		}
		active[id] = c
	}

	m.chains = active
	return nil
}

func buildDripMap(cfg *config.ChainsConfig) map[string]chainregistry.DripPolicy {
	m := make(map[string]chainregistry.DripPolicy, len(cfg.Chains))
	for i := range cfg.Chains {
		c := &cfg.Chains[i]
		m[c.ChainID] = chainregistry.DripPolicy{
			Anonymous:           c.Drip.Anonymous,
			Signed:              c.Drip.Signed,
			MaxPerAddressPerDay: c.Drip.MaxPerAddressPerDay,
			Memo:                c.Drip.Memo,
		}
	}
	return m
}

func (m *Manager) dripFor(chainID string) chainregistry.DripPolicy {
	return m.drips[chainID]
}

func (m *Manager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.chains {
		c.Close()
	}
	m.chains = make(map[string]*Chain)
}
