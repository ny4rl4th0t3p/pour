package chainregistry

// resolveUnsafe builds a resolved *ChainInfo for a registry chain by applying
// the live snapshot then local overrides. No policy is applied here — policy
// is enforced by UpdateLive when comparing old vs new resolved state.
// Must be called with s.mu held.
func (s *Store) resolveUnsafe(chainID string) (*ChainInfo, error) {
	if s.live == nil {
		return nil, ErrChainNotFound
	}
	raw, ok := s.live.Chains[chainID]
	if !ok {
		return nil, ErrChainNotFound
	}
	info := rawToInfo(raw)
	var sources FieldSources
	sources.setAll(SourceLive)

	if s.overrides != nil {
		if ov, ok := s.overrides.Chains[chainID]; ok {
			applyOverride(&info, &sources, ov)
		}
	}
	info.Sources = sources
	return &info, nil
}

// resolveStandalone builds a resolved *ChainInfo for a standalone chain by
// applying local overrides on top of the config-supplied base. Must be called
// with s.mu held.
func (s *Store) resolveStandalone(base ChainInfo) *ChainInfo {
	resolved := base
	var sources FieldSources
	sources.setAll(SourceConfig)
	if s.overrides != nil {
		if ov, ok := s.overrides.Chains[base.ChainID]; ok {
			applyOverride(&resolved, &sources, ov)
		}
	}
	resolved.Sources = sources
	return &resolved
}

func feeTokenDenoms(fts []FeeToken) []string {
	out := make([]string, len(fts))
	for i, ft := range fts {
		out[i] = ft.Denom
	}
	return out
}
