package config

import "testing"

// G-056: an affiliate network template keeps the token names its cabinet uses,
// so the admin can print a ready postback link for the operator.
func TestNetworkValidate(t *testing.T) {
	g := Group{
		ID: "g",
		Offers: []Offer{
			{ID: "o1", URL: "https://a", Status: true, NetworkID: "sticky"},
			{ID: "plain", URL: "https://b", Status: true},
		},
		Networks: []Network{{
			ID: "sticky", Name: "StickyAds",
			CIDToken: "{sub1}", ProfitToken: "{payout}", StatusToken: "{status}", TxnToken: "{txn}",
		}},
	}
	if err := g.ValidateNetworks(); err != nil {
		t.Errorf("valid network: %v", err)
	}

	cases := map[string]Group{
		"network without id": {Networks: []Network{{Name: "net"}}},
		"bad network id":     {Networks: []Network{{ID: "net work"}}},
		"duplicate network":  {Networks: []Network{{ID: "a"}, {ID: "a"}}},
		"token without braces": {Networks: []Network{{
			ID: "a", CIDToken: "subid",
		}}},
		"token with a space": {Networks: []Network{{
			ID: "a", CIDToken: "{sub id}",
		}}},
		"duplicate tokens": {Networks: []Network{{
			ID: "a", CIDToken: "{x}", ProfitToken: "{x}",
		}}},
		"offer points at a missing network": {Offers: []Offer{{
			ID: "o1", URL: "https://a", Status: true, NetworkID: "nope",
		}}},
	}
	for name, g := range cases {
		if err := g.ValidateNetworks(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestNetworkLookupDefaults(t *testing.T) {
	g := Group{Networks: []Network{{ID: "plain", Name: "Plain"}}}
	n, ok := g.Network("plain")
	if !ok || n.Name != "Plain" {
		t.Fatalf("Network(plain) = (%+v,%v)", n, ok)
	}
	cid, profit, status, txn := n.Tokens()
	if cid != "{subid}" || profit != "{payout}" || status != "{status}" || txn != "{txn}" {
		t.Errorf("default tokens = %q %q %q %q", cid, profit, status, txn)
	}
	custom := Network{CIDToken: "{sub1}", ProfitToken: "{amount}"}
	cid, profit, status, txn = custom.Tokens()
	if cid != "{sub1}" || profit != "{amount}" || status != "{status}" || txn != "{txn}" {
		t.Errorf("custom tokens = %q %q %q %q", cid, profit, status, txn)
	}
	if _, ok := g.Network(""); ok {
		t.Error("empty id reported found")
	}
	if _, ok := g.Network("nope"); ok {
		t.Error("unknown id reported found")
	}
}
