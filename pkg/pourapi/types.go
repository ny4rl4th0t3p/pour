package pourapi

// PourRequest is the body for POST /v1/pour.
type PourRequest struct {
	ChainID string `json:"chain_id"`
	Address string `json:"address"`
}

// PourResponse is returned on a successful POST /v1/pour.
type PourResponse struct {
	DripID int64  `json:"drip_id"`
	Status string `json:"status"` // "confirmed"
	Amount string `json:"amount"` // e.g. "1000000uosmo"
	TxHash string `json:"tx_hash"`
}

// InfoResponse is returned by GET /v1/info.
type InfoResponse struct {
	Version             string    `json:"version"`
	RegistryLastFetched string    `json:"registry_last_fetched"`
	RegistryRefreshMode string    `json:"registry_refresh_mode"`
	Abuse               AbuseInfo `json:"abuse"`
}

// AbuseInfo describes which abuse-prevention mechanisms are active.
type AbuseInfo struct {
	PoWEnabled                bool `json:"pow_enabled"`
	APIKeysEnabled            bool `json:"api_keys_enabled"`
	SignatureChallengeEnabled bool `json:"signature_challenge_enabled"`
}

// ChainInfo describes a single enabled chain for GET /v1/chains.
type ChainInfo struct {
	ChainID      string `json:"chain_id"`
	Bech32Prefix string `json:"bech32_prefix"`
	DripAmount   string `json:"drip_amount"`
}

// ChainsResponse is returned by GET /v1/chains.
type ChainsResponse struct {
	Chains []ChainInfo `json:"chains"`
}

// ChainDetailResponse is returned by GET /v1/chains/{chain_id}.
type ChainDetailResponse struct {
	ChainID      string `json:"chain_id"`
	ChainName    string `json:"chain_name"`
	Bech32Prefix string `json:"bech32_prefix"`
	Slip44       uint32 `json:"slip44"`
	DripAmount   string `json:"drip_amount"`
	LastChanged  string `json:"last_changed"` // RFC3339; empty if never updated
}

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// ErrorResponse wraps a human-readable error message.
type ErrorResponse struct {
	Error string `json:"error"`
}
