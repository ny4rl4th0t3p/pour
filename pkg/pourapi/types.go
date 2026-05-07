package pourapi

// PourStatus values for POST /v1/pour responses.
const (
	StatusQueued    = "queued"
	StatusConfirmed = "confirmed"
	StatusFailed    = "failed"
)

// PowCredential carries the Altcha challenge and client-solved solution for the PoW mechanism.
type PowCredential struct {
	Challenge string `json:"challenge"`
	Solution  string `json:"solution"`
}

// SigCredential carries the signed-challenge credentials for the signed mechanism.
// Nonce is the value from GET /v1/sign/nonce that the client signed.
type SigCredential struct {
	Nonce     string `json:"nonce"`
	Address   string `json:"address"`
	Pubkey    string `json:"pubkey"`
	Signature string `json:"signature"`
}

// PourRequest is the body for POST /v1/pour.
type PourRequest struct {
	ChainID   string         `json:"chain_id"`
	Address   string         `json:"address"`
	Pow       *PowCredential `json:"pow,omitempty"`
	Signature *SigCredential `json:"signature,omitempty"`
}

// PourResponse is returned on a successful POST /v1/pour.
// Status is "queued" (async batch path) or "confirmed" (sync path).
// TxHash is omitted when status is "queued".
type PourResponse struct {
	DripID    int64  `json:"drip_id"`
	Status    string `json:"status"`
	Amount    string `json:"amount"` // e.g. "1000000uosmo"
	Mechanism string `json:"mechanism"`
	TxHash    string `json:"tx_hash,omitempty"`
}

// ChallengeResponse is returned by GET /v1/pow/challenge.
type ChallengeResponse struct {
	Challenge string `json:"challenge"`
}

// NonceResponse is returned by GET /v1/sign/nonce.
type NonceResponse struct {
	Nonce string `json:"nonce"`
}

// InfoResponse is returned by GET /v1/info.
type InfoResponse struct {
	Version             string    `json:"version"`
	RegistryLastFetched string    `json:"registry_last_fetched"`
	RegistryRefreshMode string    `json:"registry_refresh_mode"`
	PendingFrozenCount  int       `json:"pending_frozen_count"`
	IBCChannelCount     int       `json:"ibc_channel_count"`
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

// IBCChannelInfo describes one IBC transfer channel from a single chain's perspective.
type IBCChannelInfo struct {
	PeerChainName string `json:"peer_chain_name"`
	ChannelID     string `json:"channel_id"`      // this chain's channel
	PeerChannelID string `json:"peer_channel_id"` // peer chain's channel
	PortID        string `json:"port_id"`
	Status        string `json:"status"`
	Preferred     bool   `json:"preferred"`
}

// ChainDetailResponse is returned by GET /v1/chains/{chain_id}.
type ChainDetailResponse struct {
	ChainID      string           `json:"chain_id"`
	ChainName    string           `json:"chain_name"`
	Bech32Prefix string           `json:"bech32_prefix"`
	Slip44       uint32           `json:"slip44"`
	DripAmount   string           `json:"drip_amount"`
	LastChanged  string           `json:"last_changed"` // RFC3339; empty if never updated
	IBCChannels  []IBCChannelInfo `json:"ibc_channels"`
}

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// ErrorResponse wraps a human-readable error message.
type ErrorResponse struct {
	Error string `json:"error"`
}
