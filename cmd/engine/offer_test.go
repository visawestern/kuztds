package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/logbuf"
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
		cur    string
		offer  string
		want   float64
		wantOK bool
	}{
		{"postback profit wins", 9, true, "", "usd", 9, true},
		{"explicit zero in the message wins", 0, true, "", "usd", 0, true},
		{"postback amount in EUR converted", 10, true, "EUR", "usd", 11, true},
		{"postback amount in USD", 10, true, "USD", "eur", 10, true},
		{"offer payout in USD", 0, false, "", "usd", 2.5, true},
		{"offer payout converted", 0, false, "", "eur", 2.75, true},
		{"currency is ignored without an amount", 0, false, "EUR", "usd", 2.5, true},
		{"offer without payout", 0, false, "", "nopay", 0, true},
		{"unknown offer", 0, false, "", "missing", 0, true},
		{"no offer at all", 0, false, "", "", 0, true},
	}
	for _, c := range cases {
		pbd := postbackData{Profit: c.profit, HasProfit: c.has, Currency: c.cur}
		got, ok := effectiveProfit(pbd, grp, c.offer, rates)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: effectiveProfit = (%v,%v), want (%v,%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
	// A rate-less currency must say so: the caller records 0 and logs why.
	grp2 := config.Group{Offers: []config.Offer{{ID: "x", Status: true, Payout: 2.5, Currency: "GBP"}}}
	if got, ok := effectiveProfit(postbackData{}, grp2, "x", rates); got != 0 || ok {
		t.Errorf("GBP without a rate = (%v,%v), want (0,false)", got, ok)
	}
	if got, ok := effectiveProfit(postbackData{Profit: 3, HasProfit: true, Currency: "GBP"}, grp2, "x", rates); got != 0 || ok {
		t.Errorf("postback GBP without a rate = (%v,%v), want (0,false)", got, ok)
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

// G-051: a stream may point at several offers; the stream's distribution picks
// one per visit, and ids the flow cannot serve are skipped.
func TestOfferListRotation(t *testing.T) {
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{
			{ID: "o1", URL: "https://one.example", Status: true},
			{ID: "o2", URL: "https://two.example", Status: true},
		},
		Streams: []config.Stream{{Name: "s", Status: true,
			Out: config.Output{Redirect: "offer", Out: "o1|||o2", Distribution: "rotator"}}},
	})
	h, ins, stop := curlEnv(t, g, "0")

	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	first := rec.Header().Get("Location")
	req := httptest.NewRequest(http.MethodGet, "/g", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	req.Header.Set("User-Agent", uaWinChrome)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	second := rec2.Header().Get("Location")
	stop()
	if first != "https://one.example" {
		t.Fatalf("first visit → %q, want the first offer", first)
	}
	if second != "https://two.example" {
		t.Errorf("second visit → %q, want the second offer", second)
	}
	if len(ins.events) != 2 || ins.events[0].Offer != "o1" || ins.events[1].Offer != "o2" {
		t.Errorf("logged offers = %v, want o1 then o2", offersOf(ins.events))
	}
}

func TestOfferListSkipsBroken(t *testing.T) {
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{
			{ID: "draft", Status: false},
			{ID: "o2", URL: "https://two.example", Status: true},
		},
		Streams: []config.Stream{{Name: "s", Status: true,
			Out: config.Output{Redirect: "offer", Out: "missing|||draft|||o2"}}},
	})
	h, _, stop := curlEnv(t, g, "0")
	defer stop()
	for i := 0; i < 5; i++ {
		rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
		if got := rec.Header().Get("Location"); got != "https://two.example" {
			t.Fatalf("visit %d → %q, want the only servable offer", i, got)
		}
	}

	none := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers:  []config.Offer{{ID: "draft", Status: false}},
		Streams: []config.Stream{{Name: "s", Status: true, Out: config.Output{Redirect: "offer", Out: "missing|||draft"}}},
	})
	h2, _, stop2 := curlEnv(t, none, "0")
	defer stop2()
	rec := do(t, h2, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if rec.Code != 200 || rec.Header().Get("Location") != "" {
		t.Errorf("no servable offers: got %d %q, want the trash answer", rec.Code, rec.Header().Get("Location"))
	}
}

func offersOf(evs []logbuf.Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Offer)
	}
	return out
}

// A rotated offer that does not buy the visitor's country still goes through
// the country fallback, not straight to the trash.
func TestOfferListRotationKeepsCountryRules(t *testing.T) {
	g := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{
			{ID: "de", URL: "https://de.example", Status: true, Countries: []string{"DE"}, Fallback: "any"},
			{ID: "any", URL: "https://any.example", Status: true},
		},
		Streams: []config.Stream{{Name: "s", Status: true,
			Out: config.Output{Redirect: "offer", Out: "de|||any", Distribution: "rotator"}}},
	})
	h, _, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome, "CF-IPCountry": "US"})
	stop()
	if got := rec.Header().Get("Location"); got != "https://any.example" {
		t.Errorf("US visitor on the first offer → %q, want the country fallback", got)
	}
}

// G-130: an offer is a partner's URL; the flow's replacement must not rewrite it.
func TestOfferURLNotRewritten(t *testing.T) {
	g := offerGroups(config.Offer{ID: "o1", Status: true, URL: "https://net.example/?slot=KEEP"}, true)
	g.List()[0].Streams[0].URLReplace = []config.URLEdit{{Find: "KEEP", Replace: "CHANGED"}}
	h, _, stop := curlEnv(t, g, "0")
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	stop()
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "slot=KEEP") {
		t.Errorf("offer Location = %q, want the offer URL untouched", loc)
	}
}
