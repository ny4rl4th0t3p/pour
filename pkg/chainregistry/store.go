package chainregistry

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Options configures a new Store.
type Options struct {
	// Overrides is the parsed operator chains.yml. May be nil.
	Overrides *OverrideSet

	// Logger is optional; defaults to slog.Default().
	Logger *slog.Logger
}

// Store is the package's main type. It holds the composed resolved view of all
// configured chains and the source data needed to re-resolve after updates.
//
// Store is goroutine-safe. Get returns an immutable *ChainInfo pointer that
// callers may cache across calls without synchronization.
type Store struct {
	chains         map[string]*ChainInfo // resolved view, keyed by chain ID
	standaloneBase map[string]ChainInfo  // pre-override base for standalone chains
	standalone     map[string]struct{}   // chain IDs that came from config, not registry
	live           *Snapshot             // nil until first UpdateLive
	ibcChannels    []IBCChannel          // ICS20 channels from the last live snapshot
	overrides      *OverrideSet

	// pending holds freeze-policy changes awaiting Accept, keyed by chainID+":"+field.
	// Map ensures at most one pending entry per (chain, field).
	pending map[string]*PendingChange

	log *slog.Logger
	mu  sync.RWMutex
}

// New creates an empty Store. Chains are populated via AddStandalone and UpdateLive.
func New(opts Options) (*Store, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Store{
		chains:         make(map[string]*ChainInfo),
		standaloneBase: make(map[string]ChainInfo),
		standalone:     make(map[string]struct{}),
		overrides:      opts.Overrides,
		pending:        make(map[string]*PendingChange),
		log:            opts.Logger,
	}, nil
}

// AddStandalone registers chains that are fully defined in operator config and
// have no registry entry. Their resolved view is built from the supplied ChainInfo
// values with current overrides applied on top. Standalone chains are never
// overwritten by UpdateLive.
func (s *Store) AddStandalone(infos ...ChainInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range infos {
		s.standaloneBase[infos[i].ChainID] = infos[i]
		s.standalone[infos[i].ChainID] = struct{}{}
		resolved := s.resolveStandalone(infos[i])
		resolved.LastChanged = now
		s.chains[infos[i].ChainID] = resolved
	}
}

// Get returns the resolved *ChainInfo for the given chain ID.
// The returned pointer is immutable; callers may hold it across calls.
// Returns ErrChainNotFound if the chain is not in the store.
func (s *Store) Get(chainID string) (*ChainInfo, error) {
	s.mu.RLock()
	info := s.chains[chainID]
	s.mu.RUnlock()
	if info == nil {
		return nil, ErrChainNotFound
	}
	return info, nil
}

// List returns all resolved chains, sorted by chain ID.
func (s *Store) List() []*ChainInfo {
	s.mu.RLock()
	out := make([]*ChainInfo, 0, len(s.chains))
	for _, info := range s.chains {
		out = append(out, info)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ChainID < out[j].ChainID })
	return out
}

// IDs returns all known chain IDs, sorted.
func (s *Store) IDs() []string {
	s.mu.RLock()
	ids := make([]string, 0, len(s.chains))
	for id := range s.chains {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

// UpdateLive incorporates a newly-fetched live snapshot into the resolved view.
// Standalone chains are never touched. On the first call (store was empty), all
// chains are populated directly with no policy applied. On subsequent calls,
// HotReload and Warn fields are applied immediately; Freeze fields are enqueued
// as PendingChange values and not applied until Accept is called.
// Returns a ChangeSet summarizing what changed, partitioned by policy.
func (s *Store) UpdateLive(snap *Snapshot) (*ChangeSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prevLive := s.live
	s.live = snap
	s.ibcChannels = snap.IBCChannels
	cs := &ChangeSet{}
	now := time.Now()

	for chainID := range snap.Chains {
		if _, isStandalone := s.standalone[chainID]; isStandalone {
			continue
		}
		newInfo, err := s.resolveUnsafe(chainID)
		if err != nil || newInfo == nil {
			continue
		}
		old, existed := s.chains[chainID]
		if !existed || prevLive == nil {
			newInfo.LastChanged = now
			s.chains[chainID] = newInfo
			continue
		}
		s.applyChainUpdate(chainID, old, newInfo, cs, now)
	}

	// Disable registry chains that disappeared from the new snapshot.
	for chainID, info := range s.chains {
		if _, isStandalone := s.standalone[chainID]; isStandalone {
			continue
		}
		if _, inSnapshot := snap.Chains[chainID]; inSnapshot {
			continue
		}
		if info.Enabled {
			disabled := *info
			disabled.Enabled = false
			s.chains[chainID] = &disabled
			cs.Removed = append(cs.Removed, chainID)
			s.log.Warn("chain disappeared from registry, disabling", "chain_id", chainID)
		}
	}

	return cs, nil
}

// Pending returns all freeze-policy changes awaiting operator acceptance.
func (s *Store) Pending() []*PendingChange {
	s.mu.RLock()
	out := make([]*PendingChange, 0, len(s.pending))
	for _, pc := range s.pending {
		out = append(out, pc)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChainID != out[j].ChainID {
			return out[i].ChainID < out[j].ChainID
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// Accept applies a single pending freeze-policy change to the resolved view
// and removes it from the pending queue.
func (s *Store) Accept(chainID, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := chainID + ":" + field
	pc, ok := s.pending[key]
	if !ok {
		return fmt.Errorf("%w: no pending change for chain %q field %q", ErrPendingChange, chainID, field)
	}

	info := s.chains[chainID]
	if info == nil {
		return fmt.Errorf("%w: chain %q not found", ErrChainNotFound, chainID)
	}

	updated := *info
	if err := applyAcceptedField(&updated, field, pc.NewValue); err != nil {
		return err
	}
	s.chains[chainID] = &updated
	delete(s.pending, key)
	return nil
}

// SetOverrides replaces the current override set and re-resolves all chains.
// Called by the daemon on POST /admin/reload.
func (s *Store) SetOverrides(ov *OverrideSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides = ov
	now := time.Now()
	for chainID := range s.chains {
		old := s.chains[chainID]
		var newInfo *ChainInfo
		if _, isStandalone := s.standalone[chainID]; isStandalone {
			newInfo = s.resolveStandalone(s.standaloneBase[chainID])
		} else {
			newInfo, _ = s.resolveUnsafe(chainID)
		}
		if newInfo == nil {
			continue
		}
		changed := false
		for _, field := range classifiableFields {
			if _, _, c := fieldValues(old, newInfo, field); c {
				changed = true
				break
			}
		}
		if changed {
			newInfo.LastChanged = now
		} else {
			newInfo.LastChanged = old.LastChanged
		}
		s.chains[chainID] = newInfo
	}
}

// AllIBCChannels returns a snapshot of every known IBC channel (unique pairs,
// not per-chain endpoints). The slice is safe to use after the call returns.
func (s *Store) AllIBCChannels() []IBCChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IBCChannel, len(s.ibcChannels))
	copy(out, s.ibcChannels)
	return out
}

// ChannelsFor returns all IBC channels where the given chain name (registry
// chain_name, not chain_id) is one of the two sides.
func (s *Store) ChannelsFor(chainName string) []IBCChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []IBCChannel
	for i := range s.ibcChannels {
		if _, _, _, ok := s.ibcChannels[i].ChannelFor(chainName); ok {
			out = append(out, s.ibcChannels[i])
		}
	}
	return out
}

// applyChainUpdate diffs old vs newInfo under the field-change policy and
// records hot-reload, warn, and freeze actions into cs. Must be called with s.mu held.
func (s *Store) applyChainUpdate(chainID string, old, newInfo *ChainInfo, cs *ChangeSet, now time.Time) {
	hasChanged := false
	for _, field := range classifiableFields {
		ov, nv, changed := fieldValues(old, newInfo, field)
		if !changed {
			// Registry reverted to the applied value — clear any stale pending entry.
			delete(s.pending, chainID+":"+field)
			continue
		}
		hasChanged = true
		fc := FieldChange{ChainID: chainID, Field: field, OldValue: ov, NewValue: nv}
		switch classify(field) {
		case FieldPolicyHotReload:
			cs.HotReloaded = append(cs.HotReloaded, fc)
		case FieldPolicyWarn:
			cs.Warned = append(cs.Warned, fc)
		case FieldPolicyFreeze:
			cs.Frozen = append(cs.Frozen, fc)
			key := chainID + ":" + field
			s.pending[key] = &PendingChange{
				ChainID: chainID, Field: field,
				OldValue: ov, NewValue: nv,
				DetectedAt: now, Source: SourceLive,
			}
			// Restore old value: Freeze means do not apply until accepted.
			applyAcceptedField(newInfo, field, ov) //nolint:errcheck // only freeze fields passed, all handled
		}
	}
	if hasChanged {
		newInfo.LastChanged = now
	} else {
		newInfo.LastChanged = old.LastChanged
	}
	s.chains[chainID] = newInfo
}

// classifiableFields is the ordered list of field paths that UpdateLive diffs
// when applying policy to live registry updates.
var classifiableFields = []string{
	FieldChainID, FieldChainName, FieldNetworkType, FieldPrettyName,
	FieldBech32Prefix, FieldSlip44, FieldKeyAlgo,
	FieldEndpointsGRPC, FieldEndpointsRPC, FieldEndpointsREST,
	FieldBlockTime,
	FieldFeeTokensDenom,
	FieldFeeTokensLowGasPrice, FieldFeeTokensAvgGasPrice, FieldFeeTokensHighGasPrice,
	FieldFeeTokensDisplay, FieldFeeTokensExponent,
}
