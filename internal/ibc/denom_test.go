package ibc

import "testing"

func TestDenom_AtomOnOsmosis(t *testing.T) {
	// Well-known vector: uatom received on Osmosis via its channel-0 to Cosmos Hub.
	// SHA256("transfer/channel-0/uatom") — documented in ibc-go and Osmosis chain registry.
	const want = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2"
	if got := Denom("transfer", "channel-0", "uatom"); got != want {
		t.Errorf("Denom = %q, want %q", got, want)
	}
}

func TestDenom_OsmoOnCosmosHub(t *testing.T) {
	// Well-known vector: uosmo received on Cosmos Hub via its channel-141 to Osmosis.
	// SHA256("transfer/channel-141/uosmo") — documented in Cosmos Hub chain registry.
	const want = "ibc/14F9BC3E44B8A9C1BE1FB08980FAB87034C9905EF17CF2F5008FC085218811CC"
	if got := Denom("transfer", "channel-141", "uosmo"); got != want {
		t.Errorf("Denom = %q, want %q", got, want)
	}
}

func TestDenom_Deterministic(t *testing.T) {
	a := Denom("transfer", "channel-0", "uatom")
	b := Denom("transfer", "channel-0", "uatom")
	if a != b {
		t.Errorf("Denom is not deterministic: %q != %q", a, b)
	}
}
