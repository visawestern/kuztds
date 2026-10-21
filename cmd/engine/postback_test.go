package main

import (
	"net/http"
	"net/url"
	"testing"
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
