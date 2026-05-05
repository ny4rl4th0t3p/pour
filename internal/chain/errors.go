package chain

import "errors"

var (
	ErrChainSuspended = errors.New("chain: suspended")
	ErrSyncMode       = errors.New("chain: sync mode, use BuildAndBroadcast directly")
)
