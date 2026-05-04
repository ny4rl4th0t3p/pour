package chainregistry

// ChangeSet summarizes the field changes produced by a single UpdateLive call,
// partitioned by policy. The caller (internal/chain manager) uses this to log
// warnings and notify operators of pending freeze-policy changes.
type ChangeSet struct {
	HotReloaded []FieldChange
	Warned      []FieldChange
	Frozen      []FieldChange
}

// Empty reports whether the ChangeSet contains no changes.
func (cs *ChangeSet) Empty() bool {
	return len(cs.HotReloaded) == 0 && len(cs.Warned) == 0 && len(cs.Frozen) == 0
}

// FieldChange records one field that changed between the previous resolved view
// and the newly resolved view after a live registry update.
type FieldChange struct {
	ChainID  string
	Field    string
	OldValue any
	NewValue any
}
