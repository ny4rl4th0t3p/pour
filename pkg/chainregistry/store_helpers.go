package chainregistry

import (
	"fmt"
	"reflect"
)

// fieldValues returns the old and new values of a named field path across two
// ChainInfo values, and whether they differ. Used by UpdateLive to categorize
// HotReload and Warn changes for the ChangeSet.
func fieldValues(old, cur *ChainInfo, field string) (oldVal, newVal any, changed bool) {
	switch field {
	case "ChainID":
		return old.ChainID, cur.ChainID, old.ChainID != cur.ChainID
	case "ChainName":
		return old.ChainName, cur.ChainName, old.ChainName != cur.ChainName
	case "NetworkType":
		return old.NetworkType, cur.NetworkType, old.NetworkType != cur.NetworkType
	case "PrettyName":
		return old.PrettyName, cur.PrettyName, old.PrettyName != cur.PrettyName
	case "Bech32Prefix":
		return old.Bech32Prefix, cur.Bech32Prefix, old.Bech32Prefix != cur.Bech32Prefix
	case "Slip44":
		return old.Slip44, cur.Slip44, old.Slip44 != cur.Slip44
	case "KeyAlgo":
		return old.KeyAlgo, cur.KeyAlgo, old.KeyAlgo != cur.KeyAlgo
	case "Endpoints.GRPC":
		eq := reflect.DeepEqual(old.Endpoints.GRPC, cur.Endpoints.GRPC)
		return old.Endpoints.GRPC, cur.Endpoints.GRPC, !eq
	case "Endpoints.RPC":
		eq := reflect.DeepEqual(old.Endpoints.RPC, cur.Endpoints.RPC)
		return old.Endpoints.RPC, cur.Endpoints.RPC, !eq
	case "Endpoints.REST":
		eq := reflect.DeepEqual(old.Endpoints.REST, cur.Endpoints.REST)
		return old.Endpoints.REST, cur.Endpoints.REST, !eq
	case "BlockTime":
		return old.BlockTime, cur.BlockTime, old.BlockTime != cur.BlockTime
	case "FeeTokens.Denom":
		ods, nds := feeTokenDenoms(old.FeeTokens), feeTokenDenoms(cur.FeeTokens)
		return old.FeeTokens, cur.FeeTokens, !reflect.DeepEqual(ods, nds)
	case "FeeTokens.LowGasPrice":
		os, ns := feeTokenGasSummary(old.FeeTokens, "low"), feeTokenGasSummary(cur.FeeTokens, "low")
		return os, ns, os != ns
	case "FeeTokens.AverageGasPrice":
		os, ns := feeTokenGasSummary(old.FeeTokens, "avg"), feeTokenGasSummary(cur.FeeTokens, "avg")
		return os, ns, os != ns
	case "FeeTokens.HighGasPrice":
		os, ns := feeTokenGasSummary(old.FeeTokens, "high"), feeTokenGasSummary(cur.FeeTokens, "high")
		return os, ns, os != ns
	case "FeeTokens.Display":
		os, ns := feeTokenDisplaySummary(old.FeeTokens), feeTokenDisplaySummary(cur.FeeTokens)
		return os, ns, os != ns
	case "FeeTokens.Exponent":
		eq := reflect.DeepEqual(old.FeeTokens, cur.FeeTokens)
		return old.FeeTokens, cur.FeeTokens, !eq
	}
	return nil, nil, false
}

func feeTokenGasSummary(fts []FeeToken, kind string) string {
	out := ""
	for _, ft := range fts {
		switch kind {
		case "low":
			out += ft.Denom + "=" + ft.LowGasPrice.String() + ";"
		case "avg":
			out += ft.Denom + "=" + ft.AverageGasPrice.String() + ";"
		case "high":
			out += ft.Denom + "=" + ft.HighGasPrice.String() + ";"
		}
	}
	return out
}

func feeTokenDisplaySummary(fts []FeeToken) string {
	out := ""
	for _, ft := range fts {
		out += ft.Denom + "=" + ft.Display + ";"
	}
	return out
}

// applyAcceptedField writes a previously-pending freeze-policy change's new
// value into info. Used by Accept to apply the accepted value immediately.
func applyAcceptedField(info *ChainInfo, field string, newValue any) error {
	switch field {
	case "ChainID":
		if v, ok := newValue.(string); ok {
			info.ChainID = v
		}
	case "ChainName":
		if v, ok := newValue.(string); ok {
			info.ChainName = v
		}
	case "NetworkType":
		switch v := newValue.(type) {
		case NetworkType:
			info.NetworkType = v
		case string:
			info.NetworkType = NetworkType(v)
		}
	case "Bech32Prefix":
		if v, ok := newValue.(string); ok {
			info.Bech32Prefix = v
		}
	case "Slip44":
		if v, ok := newValue.(uint32); ok {
			info.Slip44 = v
		}
	case "KeyAlgo":
		switch v := newValue.(type) {
		case KeyAlgo:
			info.KeyAlgo = v
		case string:
			info.KeyAlgo = KeyAlgo(v)
		}
	case "FeeTokens.Denom":
		if v, ok := newValue.([]FeeToken); ok {
			info.FeeTokens = v
		}
	default:
		return fmt.Errorf("chainregistry: applyAcceptedField: unhandled field %q", field)
	}
	return nil
}
