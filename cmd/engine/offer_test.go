package main

import (
	"strings"
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
)

// G-048: поток может вести на предложение кампании (redirect "offer").
// Ссылка предложения получает те же макросы, что и обычный вывод.

func offerGroups(offer config.Offer, status bool) *config.Groups {
	return config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers:  []config.Offer{offer},
		Streams: []config.Stream{{Name: "s", Status: status, Out: config.Output{Redirect: "offer", Out: offer.ID}}},
	})
}

func TestOfferRedirectExpandsMacros(t *testing.T) {
	g := offerGroups(config.Offer{
		ID: "o1", Name: "Main", Network: "net", Status: true,
		URL: "https://net.example/click?cid=[CID]&p=[PAR-1]&c=[COUNTRY]",
	}, true)
	h, ins, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g?p1=abc", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if rec.Code != 302 {
		t.Fatalf("offer redirect → 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://net.example/click?cid=") ||
		!strings.Contains(loc, "&p=abc") || !strings.Contains(loc, "&c=") {
		t.Errorf("Location = %q, want the offer URL with macros expanded", loc)
	}
	if strings.Contains(loc, "[") {
		t.Errorf("Location = %q, macro left unexpanded", loc)
	}
	if len(ins.events) != 1 || ins.events[0].Offer != "o1" {
		t.Errorf("logged offer = %+v, want o1", ins.events)
	}
}

func TestOfferDisabledServesTrash(t *testing.T) {
	g := offerGroups(config.Offer{ID: "o1", URL: "https://net.example", Status: false}, true)
	h, _, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if rec.Code != 200 || rec.Header().Get("Location") != "" {
		t.Errorf("disabled offer: got %d %q, want the trash answer (200, no redirect)", rec.Code, rec.Header().Get("Location"))
	}
}

func TestOfferUnknownServesTrash(t *testing.T) {
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Streams: []config.Stream{{Name: "s", Status: true, Out: config.Output{Redirect: "offer", Out: "missing"}}},
	})
	h, _, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if rec.Code != 200 || rec.Header().Get("Location") != "" {
		t.Errorf("unknown offer: got %d %q, want the trash answer", rec.Code, rec.Header().Get("Location"))
	}
}

func TestOfferAsGroupDefault(t *testing.T) {
	// Поток без своего вывода наследует групповой default типа offer.
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "offer", Out: "o1",
		Offers:  []config.Offer{{ID: "o1", URL: "https://net.example/x", Status: true}},
		Streams: []config.Stream{{Name: "s", Status: true}},
	})
	h, _, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if rec.Code != 302 || rec.Header().Get("Location") != "https://net.example/x" {
		t.Errorf("group default offer: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// G-049: revenue of a conversion — the postback's profit wins; when it is
// absent, the served offer's payout is taken, in the offer's currency.
func TestEffectiveProfit(t *testing.T) {
	grp := config.Group{Offers: []config.Offer{
		{ID: "usd", Status: true, Payout: 2.5},
		{ID: "eur", Status: true, Payout: 2.5, Currency: "EUR"},
		{ID: "nopay", Status: true},
	}}
	rates := map[string]float64{"EUR": 1.1}
	cases := []struct {
		name   string
		profit float64
		has    bool
		offer  string
		want   float64
		wantOK bool
	}{
		{"postback profit wins", 9, true, "usd", 9, true},
		{"explicit zero in the message wins", 0, true, "usd", 0, true},
		{"offer payout in USD", 0, false, "usd", 2.5, true},
		{"offer payout converted", 0, false, "eur", 2.75, true},
		{"offer without payout", 0, false, "nopay", 0, true},
		{"unknown offer", 0, false, "missing", 0, true},
		{"no offer at all", 0, false, "", 0, true},
	}
	for _, c := range cases {
		got, ok := effectiveProfit(c.profit, c.has, grp, c.offer, rates)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: effectiveProfit = (%v,%v), want (%v,%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
	// A rate-less currency must say so: the caller records 0 and logs why.
	grp2 := config.Group{Offers: []config.Offer{{ID: "x", Status: true, Payout: 2.5, Currency: "GBP"}}}
	if got, ok := effectiveProfit(0, false, grp2, "x", rates); got != 0 || ok {
		t.Errorf("GBP without a rate = (%v,%v), want (0,false)", got, ok)
	}
}

// G-054: an offer is served only to the countries it buys; otherwise the
// fallback is tried, and without one the visit gets the trash answer.
func TestOfferCountries(t *testing.T) {
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{
			{ID: "o1", URL: "https://main.example", Status: true, Countries: []string{"DE", "FR"}, Fallback: "o2"},
			{ID: "o2", URL: "https://backup.example", Status: true, Countries: []string{"US"}},
		},
		Streams: []config.Stream{{Name: "s", Status: true, Out: config.Output{Redirect: "offer", Out: "o1"}}},
	})
	h, _, stop := curlEnv(t, g, "0")
	defer stop()
	cases := map[string]string{"DE": "https://main.example", "US": "https://backup.example", "RU": ""}
	for country, want := range cases {
		rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome, "CF-IPCountry": country})
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q", country, got, want)
		}
	}

	noFallback := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers:  []config.Offer{{ID: "o1", URL: "https://main.example", Status: true, Countries: []string{"DE"}}},
		Streams: []config.Stream{{Name: "s", Status: true, Out: config.Output{Redirect: "offer", Out: "o1"}}},
	})
	h2, _, stop2 := curlEnv(t, noFallback, "0")
	defer stop2()
	rec := do(t, h2, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome, "CF-IPCountry": "US"})
	if rec.Code != 200 || rec.Header().Get("Location") != "" {
		t.Errorf("country outside the offer: got %d %q, want the trash answer", rec.Code, rec.Header().Get("Location"))
	}
}
