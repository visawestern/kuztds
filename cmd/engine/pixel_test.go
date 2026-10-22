package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConversionPixelRecordsConversion(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, c := dedupeEnvMode(t, st, "", 1)
	rec := do(t, h, "/_px.js?pb=secret&cid=c1&tid=t1&status=sale&profit=5", "8.8.8.8", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("content-type = %q, want javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
	if st.records != 1 || !st.lastRecorded {
		t.Fatalf("records = %d, recorded = %v; want one recorded conversion", st.records, st.lastRecorded)
	}
	if st.lastCID != "c1" || st.lastTID != "t1" || st.lastProfit != 5 {
		t.Fatalf("pixel payload = cid %q tid %q profit %v", st.lastCID, st.lastTID, st.lastProfit)
	}
	if st.lastMode != "update" {
		t.Fatalf("mode = %q, want update", st.lastMode)
	}
	reached, err := c.OfferCapReached(context.Background(), "g", "main", 1, time.Now())
	if err != nil || !reached {
		t.Fatalf("recorded pixel spent no offer slot: reached=%v err=%v", reached, err)
	}
}

func TestConversionPixelSharesDedupeWithPostback(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, c := dedupeEnvMode(t, st, "ignore", 2)
	postback(t, h, "cid=c1&tid=t1&status=sale&profit=5")
	rec := do(t, h, "/_px.js?pb=secret&cid=c1&tid=t1&status=sale&profit=9", "8.8.8.8", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if st.records != 2 {
		t.Fatalf("records = %d, want 2 (the pixel reached the store)", st.records)
	}
	if st.lastRecorded {
		t.Fatal("ignore mode recorded the pixel duplicate; the S2S conversion must win")
	}
	if capReached(t, c) {
		t.Fatal("the ignored pixel duplicate spent a second offer slot")
	}
}

func TestConversionPixelBadKeyDoesNotRecord(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, _ := dedupeEnv(t, st)
	rec := do(t, h, "/_px.js?pb=wrong&cid=c1&tid=t1&status=sale&profit=5", "8.8.8.8", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if st.records != 0 {
		t.Fatalf("records = %d, want 0 for a bad key", st.records)
	}
	if !strings.Contains(rec.Body.String(), "conversion pixel") {
		t.Fatalf("body = %q, want a JS comment", rec.Body.String())
	}
}

func TestConversionPixelBadStatusDoesNotRecord(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, _ := dedupeEnv(t, st)
	do(t, h, "/_px.js?pb=secret&cid=c1&tid=t1&status=bad-status&profit=5", "8.8.8.8", nil)
	if st.records != 0 {
		t.Fatalf("records = %d, want 0 for an invalid status", st.records)
	}
}

func TestConversionPixelE2E(t *testing.T) {
	st := &fakePostbackStore{group: "g1", offer: "main"}
	h := newEng(t, group(), engHarness{pbStore: st})
	rec := do(t, h, "/_px.js?pb=pb&cid=c1&tid=t1&status=sale&profit=7", "8.8.8.8", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if st.records != 1 || st.lastCID != "c1" || st.lastTID != "t1" || st.lastProfit != 7 {
		t.Fatalf("pixel did not reach the conversion pipeline: %+v", st)
	}
}

func TestConversionPixelHeadDoesNotRecord(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, _ := dedupeEnv(t, st)
	req := httptest.NewRequest(http.MethodHead, "/_px.js?pb=secret&cid=c1&tid=t1&status=sale&profit=5", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD code = %d, want 200", rec.Code)
	}
	if st.records != 0 {
		t.Fatalf("HEAD recorded %d conversions, want 0", st.records)
	}
}
