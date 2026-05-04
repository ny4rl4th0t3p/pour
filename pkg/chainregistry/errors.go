package chainregistry

import "errors"

var (
	// ErrChainNotFound is returned when the requested chain ID is not in the store.
	ErrChainNotFound = errors.New("chainregistry: chain not found")

	// ErrPendingChange is returned when a freeze-policy field change is awaiting
	// operator acceptance via Accept.
	ErrPendingChange = errors.New("chainregistry: change pending operator acceptance")

	// ErrInvalidPolicy is returned when classify is called with a field name that
	// has no entry in the policy map and the caller requires a valid policy.
	ErrInvalidPolicy = errors.New("chainregistry: invalid field policy")
)
