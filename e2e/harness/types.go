package harness

// IBCChannelInfo mirrors pourapi.IBCChannelInfo for black-box HTTP assertions.
type IBCChannelInfo struct {
	PeerChainName string `json:"peer_chain_name"`
	ChannelID     string `json:"channel_id"`
	PeerChannelID string `json:"peer_channel_id"`
	PortID        string `json:"port_id"`
	Status        string `json:"status"`
	Preferred     bool   `json:"preferred"`
}

// ChainDetailResponse mirrors pourapi.ChainDetailResponse.
type ChainDetailResponse struct {
	ChainID     string           `json:"chain_id"`
	ChainName   string           `json:"chain_name"`
	IBCChannels []IBCChannelInfo `json:"ibc_channels"`
}

// InfoResponse mirrors pourapi.InfoResponse (discovery fields only).
type InfoResponse struct {
	IBCChannelCount int `json:"ibc_channel_count"`
}

// PourResponse mirrors pourapi.PourResponse for black-box HTTP assertions.
type PourResponse struct {
	DripID int64  `json:"drip_id"`
	Status string `json:"status"`
	Amount string `json:"amount"`
	TxHash string `json:"tx_hash,omitempty"`
}

// RelayerAddr is the cosmos address for RelayerMnemonic at m/44'/118'/0'/0/0.
// Used in TestAutoMode_WaitForFunding as the pour distributor address (not in genesis).
const RelayerAddr = "cosmos19rl4cm2hmr8afy4kldpxz3fka4jguq0auqdal4"

// TestAutoRecipient is a valid cosmos address with no genesis balance.
// Used as the pour destination in auto-mode e2e tests.
const TestAutoRecipient = "cosmos1am058pdux3hyulcmfgj4m3hhrlfn8nzm88u80q"
