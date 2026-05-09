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
