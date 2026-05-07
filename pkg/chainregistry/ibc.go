package chainregistry

import "encoding/json"

// rawIBCFile mirrors the on-disk format of cosmos/chain-registry _IBC/<a>-<b>.json files.
type rawIBCFile struct {
	Chain1   rawIBCSide      `json:"chain_1"`
	Chain2   rawIBCSide      `json:"chain_2"`
	Channels []rawIBCChannel `json:"channels"`
}

type rawIBCSide struct {
	ChainName    string `json:"chain_name"`
	ClientID     string `json:"client_id"`
	ConnectionID string `json:"connection_id"`
}

type rawIBCChannel struct {
	Chain1   rawIBCChannelSide `json:"chain_1"`
	Chain2   rawIBCChannelSide `json:"chain_2"`
	Ordering string            `json:"ordering"`
	Version  string            `json:"version"`
	Tags     rawIBCTags        `json:"tags"`
}

type rawIBCChannelSide struct {
	ChannelID string `json:"channel_id"`
	PortID    string `json:"port_id"`
}

type rawIBCTags struct {
	Status    string `json:"status"`
	Preferred bool   `json:"preferred"`
}

// ibcPairFilename returns the _IBC/ filename for a channel pair. The two chain
// names are sorted alphabetically so the result is stable regardless of argument
// order, matching the cosmos/chain-registry naming convention.
func ibcPairFilename(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "-" + b + ".json"
}

// parseIBCFile converts a rawIBCFile into IBCChannel values. Only ICS20 transfer
// channels (version == "ics20-1") are returned; all others are silently skipped.
func parseIBCFile(raw rawIBCFile) []IBCChannel {
	out := make([]IBCChannel, 0, len(raw.Channels))
	for _, ch := range raw.Channels {
		if ch.Version != "ics20-1" {
			continue
		}
		out = append(out, IBCChannel{
			ChainNameA: raw.Chain1.ChainName,
			ChainNameB: raw.Chain2.ChainName,
			ChannelA:   ch.Chain1.ChannelID,
			ChannelB:   ch.Chain2.ChannelID,
			PortA:      ch.Chain1.PortID,
			PortB:      ch.Chain2.PortID,
			Ordering:   ch.Ordering,
			Version:    ch.Version,
			Status:     ch.Tags.Status,
			Preferred:  ch.Tags.Preferred,
		})
	}
	return out
}

// unmarshalIBCFile parses raw JSON bytes into IBCChannel values.
func unmarshalIBCFile(data []byte) ([]IBCChannel, error) {
	var raw rawIBCFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return parseIBCFile(raw), nil
}

// SelectChannel picks the best ICS20 transfer channel from channels for the
// given source and destination chain names. Selection priority: status=live +
// preferred=true first; falls back to the first status=live channel. Returns
// (channel, true) on success or (zero, false) if no live channel exists for
// the pair. The ambiguous return is true when a live channel was found but
// none is marked preferred — the caller should log a warning.
func SelectChannel(channels []IBCChannel, from, to string) (ch IBCChannel, ok, ambiguous bool) {
	var live []IBCChannel
	for i := range channels {
		_, _, peer, match := channels[i].ChannelFor(from)
		if !match || peer != to || channels[i].Status != "live" {
			continue
		}
		if channels[i].Preferred {
			return channels[i], true, false
		}
		live = append(live, channels[i])
	}
	if len(live) == 0 {
		return IBCChannel{}, false, false
	}
	return live[0], true, len(live) > 1
}
