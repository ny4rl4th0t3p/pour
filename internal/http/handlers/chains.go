package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ny4rl4th0t3p/pour/pkg/pourapi"
)

// Chains handles GET /v1/chains.
func (h *Handler) Chains(w http.ResponseWriter, _ *http.Request) {
	active := h.source.ListActive()
	chains := make([]pourapi.ChainInfo, 0, len(active))
	for _, snap := range active {
		chains = append(chains, pourapi.ChainInfo{
			ChainID:      snap.Info.ChainID,
			Bech32Prefix: snap.Info.Bech32Prefix,
			DripAmount:   snap.Drip.Anonymous,
		})
	}
	writeJSON(w, http.StatusOK, pourapi.ChainsResponse{Chains: chains})
}

// ChainDetail handles GET /v1/chains/{chain_id}.
func (h *Handler) ChainDetail(w http.ResponseWriter, r *http.Request) {
	chainID := chi.URLParam(r, "chain_id")
	snap, ok := h.source.GetActive(chainID)
	if !ok {
		writeError(w, http.StatusNotFound, "chain not found or not enabled")
		return
	}
	lastChanged := ""
	if t := snap.Info.LastChanged; !t.IsZero() {
		lastChanged = t.UTC().Format(time.RFC3339)
	}
	rawChannels := h.source.ChannelsFor(snap.Info.ChainName)
	ibcChannels := make([]pourapi.IBCChannelInfo, 0, len(rawChannels))
	for i := range rawChannels {
		channelID, portID, peerName, _ := rawChannels[i].ChannelFor(snap.Info.ChainName)
		peerChannelID, _, _, _ := rawChannels[i].ChannelFor(peerName)
		ibcChannels = append(ibcChannels, pourapi.IBCChannelInfo{
			PeerChainName: peerName,
			ChannelID:     channelID,
			PeerChannelID: peerChannelID,
			PortID:        portID,
			Status:        rawChannels[i].Status,
			Preferred:     rawChannels[i].Preferred,
		})
	}
	writeJSON(w, http.StatusOK, pourapi.ChainDetailResponse{
		ChainID:      snap.Info.ChainID,
		ChainName:    snap.Info.ChainName,
		Bech32Prefix: snap.Info.Bech32Prefix,
		Slip44:       snap.Info.Slip44,
		DripAmount:   snap.Drip.Anonymous,
		LastChanged:  lastChanged,
		IBCChannels:  ibcChannels,
	})
}
