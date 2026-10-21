package config

import "testing"

// G-048: карточка предложения (id, имя, сеть, ссылка, статус) в кампании.

func TestOfferValidate(t *testing.T) {
	valid := Group{ID: "g", Offers: []Offer{
		{ID: "o1", Name: "Main", Network: "net", URL: "https://net.example/?cid=[CID]", Status: true},
		{ID: "draft", Status: false}, // disabled draft: link may be empty
	}}
	if err := valid.ValidateOffers(); err != nil {
		t.Errorf("valid offers: %v", err)
	}

	// G-049: a payout is a number in an ISO currency (empty = USD).
	priced := Group{Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Payout: 1.5, Currency: "eur"}}}
	if err := priced.ValidateOffers(); err != nil {
		t.Errorf("priced offer: %v", err)
	}

	// G-050: a daily purchase cap may point at a fallback offer.
	capped := Group{Offers: []Offer{
		{ID: "o1", URL: "https://a", Status: true, CapDaily: 10, Fallback: "o2"},
		{ID: "o2", URL: "https://b", Status: true},
	}}
	if err := capped.ValidateOffers(); err != nil {
		t.Errorf("capped offer: %v", err)
	}

	cases := map[string]Group{
		"enabled without id":  {Offers: []Offer{{URL: "https://x", Status: true}}},
		"enabled without url": {Offers: []Offer{{ID: "o1", Status: true}}},
		"url without scheme":  {Offers: []Offer{{ID: "o1", URL: "net.example/x", Status: true}}},
		"url is javascript":   {Offers: []Offer{{ID: "o1", URL: "javascript:alert(1)", Status: true}}},
		"duplicate id":        {Offers: []Offer{{ID: "o1", URL: "https://a"}, {ID: "o1", URL: "https://b"}}},
		"id with spaces":      {Offers: []Offer{{ID: "o 1", URL: "https://a", Status: true}}},
		"negative payout":     {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Payout: -1}}},
		"currency is not ISO": {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Currency: "US$"}}},
		"negative daily cap":  {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, CapDaily: -1}}},
		"fallback is itself":  {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, CapDaily: 1, Fallback: "o1"}}},
		"fallback is missing": {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, CapDaily: 1, Fallback: "o2"}}},
		"fallback is disabled": {Offers: []Offer{
			{ID: "o1", URL: "https://a", Status: true, CapDaily: 1, Fallback: "o2"},
			{ID: "o2", URL: "https://b", Status: false},
		}},
	}
	for name, g := range cases {
		if err := g.ValidateOffers(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestGroupOfferLookup(t *testing.T) {
	g := Group{Offers: []Offer{{ID: "o1", URL: "https://a"}, {ID: "o2", URL: "https://b", Status: true}}}
	if o, ok := g.Offer("o1"); !ok || o.URL != "https://a" {
		t.Errorf("Offer(o1) = (%+v,%v)", o, ok)
	}
	// A disabled offer is still found: the engine decides what to do with it.
	if o, ok := g.Offer("o1"); !ok || o.Status {
		t.Errorf("disabled offer must be returned with Status=false, got %+v", o)
	}
	if _, ok := g.Offer("nope"); ok {
		t.Error("unknown offer reported found")
	}
	if _, ok := g.Offer(""); ok {
		t.Error("empty id reported found")
	}
}

// G-054: an offer may be limited to the countries its network buys.
func TestOfferCountriesValidate(t *testing.T) {
	geo := Group{Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Countries: []string{"de", "US"}}}}
	if err := geo.ValidateOffers(); err != nil {
		t.Errorf("offers with countries: %v", err)
	}
	cases := map[string]Group{
		"country is not ISO":   {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Countries: []string{"USA"}}}},
		"country with a digit": {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Countries: []string{"D1"}}}},
		"duplicate country":    {Offers: []Offer{{ID: "o1", URL: "https://a", Status: true, Countries: []string{"DE", "de"}}}},
	}
	for name, g := range cases {
		if err := g.ValidateOffers(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestOfferAcceptsCountry(t *testing.T) {
	all := Offer{ID: "all"}
	if !all.AcceptsCountry("RU") || !all.AcceptsCountry("") {
		t.Error("an offer without countries must accept everyone")
	}
	geo := Offer{ID: "geo", Countries: []string{"DE", "FR"}}
	for country, want := range map[string]bool{"DE": true, "fr": true, "US": false, "": false} {
		if got := geo.AcceptsCountry(country); got != want {
			t.Errorf("AcceptsCountry(%q) = %v, want %v", country, got, want)
		}
	}
}
