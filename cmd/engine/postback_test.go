package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/cost"
)

// Проводка status/tid (G-301…G-304) в движок.
// Keitaro 11: постбек без валидного status= игнорируется (но отвечает 200).

func TestParsePostbackValid(t *testing.T) {
	q := url.Values{"cid": {"abc"}, "profit": {"1.50"}, "status": {"sale"}, "tid": {"2"}}
	pb, ok := parsePostback(q)
	if !ok {
		t.Fatalf("valid postback → want ok")
	}
	if pb.CID != "abc" || pb.Profit != 1.50 || !pb.HasProfit || pb.Status != "sale" || pb.TID != "2" {
		t.Errorf("parsePostback = %+v, want cid=abc profit=1.5 (present) status=sale tid=2", pb)
	}
}

func TestParsePostbackTrashNormalized(t *testing.T) {
	q := url.Values{"cid": {"abc"}, "profit": {"3"}, "status": {" TRASH "}}
	pb, ok := parsePostback(q)
	if !ok || pb.Status != "trash" {
		t.Errorf("parsePostback(TRASH) = (%+v,%v), want status=trash ok", pb, ok)
	}
}

func TestParsePostbackNoStatusIgnored(t *testing.T) {
	for _, q := range []url.Values{
		{"cid": {"abc"}, "profit": {"1"}},
		{"cid": {"abc"}, "profit": {"1"}, "status": {""}},
		{"cid": {"abc"}, "profit": {"1"}, "status": {"sale'; DROP TABLE postbacks;--"}},
		{"cid": {""}, "profit": {"1"}, "status": {"sale"}},
	} {
		if _, ok := parsePostback(q); ok {
			t.Errorf("parsePostback(%v) = ok, want ignored", q)
		}
	}
}

func TestParsePostbackBadProfitIsZeroButOk(t *testing.T) {
	// Совместимость с текущим поведением: profit не парсится → 0, запись идёт.
	q := url.Values{"cid": {"abc"}, "profit": {"not-a-number"}, "status": {"lead"}}
	pb, ok := parsePostback(q)
	if !ok || pb.Profit != 0 || pb.Status != "lead" {
		t.Errorf("parsePostback(bad profit) = (%+v,%v), want profit=0 status=lead ok", pb, ok)
	}
}

func TestParsePostbackProfitPresence(t *testing.T) {
	// G-049: an absent amount falls back to the offer payout; an explicit
	// zero does not. Garbage is not an amount either.
	for _, q := range []url.Values{
		{"cid": {"abc"}, "status": {"lead"}},
		{"cid": {"abc"}, "status": {"lead"}, "profit": {"not-a-number"}},
	} {
		if pb, ok := parsePostback(q); !ok || pb.HasProfit {
			t.Errorf("parsePostback(%v) HasProfit = %v, want false", q, pb.HasProfit)
		}
	}
	if pb, ok := parsePostback(url.Values{"cid": {"abc"}, "status": {"lead"}, "profit": {"0"}}); !ok || !pb.HasProfit || pb.Profit != 0 {
		t.Errorf("explicit zero: %+v, want HasProfit with 0", pb)
	}
}

func TestParsePostbackCustomStatus(t *testing.T) {
	q := url.Values{"cid": {"abc"}, "status": {"vip_level2"}}
	pb, ok := parsePostback(q)
	if !ok || pb.Status != "vip_level2" {
		t.Errorf("parsePostback(custom) = (%+v,%v), want status=vip_level2 ok", pb, ok)
	}
}

func TestPostbackOnCPAGroupNoCHIs200(t *testing.T) {
	// Проводка цены конверсии не должна ронять запрос без ClickHouse.
	gg := costGroup("g", "cpa", 3.0, "")
	h := testEnv(t, gg, func(d *engineDeps) { d.postbackKey = "secret" })
	rec := do(t, h, "/?pb=secret&cid=abc&profit=9&status=sale&tid=1", "8.8.8.8", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("postback → 200, got %d", rec.Code)
	}
}

// G-305: payout is the compatibility alias for profit, and currency names the
// amount's ISO code. profit wins when both are present; a broken profit does
// not shadow a valid payout.
func TestParsePostbackPayoutAlias(t *testing.T) {
	cases := []struct {
		query string
		want  float64
		has   bool
		cur   string
	}{
		{"profit=1.5&payout=9", 1.5, true, ""},
		{"payout=2.25", 2.25, true, ""},
		{"profit=broken&payout=2.25", 2.25, true, ""},
		{"profit=broken", 0, false, ""},
		{"profit=NaN", 0, false, ""},
		{"payout=+Inf", 0, false, ""},
		{"profit=1&currency= eur ", 1, true, "EUR"},
		{"payout=1&currency=USD", 1, true, "USD"},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery("cid=abc&status=sale&" + c.query)
		pb, ok := parsePostback(q)
		if !ok || pb.Profit != c.want || pb.HasProfit != c.has || pb.Currency != c.cur {
			t.Errorf("%s: parsePostback = (%+v,%v), want profit=%v has=%v currency=%q", c.query, pb, ok, c.want, c.has, c.cur)
		}
	}
}

// G-305: an explicit amount is converted to USD before it reaches ClickHouse.
func TestPostbackCurrencyConvertedToUSD(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	dir := t.TempDir()
	path := filepath.Join(dir, "rates.json")
	if err := cost.SaveRates(path, map[string]float64{"USD": 1, "EUR": 1.1}, "2026-10-22"); err != nil {
		t.Fatal(err)
	}
	h := testEnv(t, config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{{ID: "main", URL: "https://main.example", Status: true}},
	}), func(d *engineDeps) {
		d.pbStore = st
		d.postbackKey = "secret"
		d.rates = newRatesStore(path, d.log)
	})
	do(t, h, "/?pb=secret&cid=c1&status=sale&tid=t1&payout=10&currency=eur", "8.8.8.8", nil)
	if st.lastProfit != 11 {
		t.Fatalf("recorded profit = %v, want 11 USD", st.lastProfit)
	}
}

// G-305: currency conversion must not depend on an offer being attached to the
// click, and a rate-less currency must never be stored as if it were USD.
func TestPostbackCurrencyWithoutOffer(t *testing.T) {
	ratesPath := filepath.Join(t.TempDir(), "rates.json")
	if err := cost.SaveRates(ratesPath, map[string]float64{"USD": 1, "EUR": 1.1}, "2026-10-22"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		offer string
		query string
		want  float64
	}{
		{"no offer, EUR", "", "payout=10&currency=EUR", 11},
		{"no offer, unknown currency", "", "payout=10&currency=GBP", 0},
		{"offer, unknown currency", "main", "profit=10&currency=GBP", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &fakePostbackStore{group: "g", offer: c.offer}
			grp := &config.Group{ID: "g", Status: true, Redirect: "stop"}
			if c.offer != "" {
				grp.Offers = []config.Offer{{ID: c.offer, URL: "https://main.example", Status: true}}
			}
			h := testEnv(t, config.NewGroups(grp), func(d *engineDeps) {
				d.pbStore = st
				d.postbackKey = "secret"
				d.rates = newRatesStore(ratesPath, d.log)
			})
			do(t, h, "/?pb=secret&cid=c1&status=sale&tid=t1&"+c.query, "8.8.8.8", nil)
			if st.lastProfit != c.want {
				t.Fatalf("recorded profit = %v, want %v", st.lastProfit, c.want)
			}
		})
	}
}
