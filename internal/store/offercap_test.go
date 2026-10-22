package store

import (
	"context"
	"testing"
	"time"
)

// G-050: purchases per offer per UTC day. The click path reads the counter;
// the postback writes it.

func TestOfferCapReached(t *testing.T) {
	c, mr := newCounters(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	mr.SetTime(now)

	if reached, err := c.OfferCapReached(ctx, "g", "o1", 2, now); reached || err != nil {
		t.Fatalf("empty day: (%v,%v), want (false,nil)", reached, err)
	}
	if reached, err := c.OfferCapReached(ctx, "g", "o1", 0, now); reached || err != nil {
		t.Fatalf("no cap: (%v,%v), want (false,nil) — unlimited", reached, err)
	}
	for i := 0; i < 2; i++ {
		if err := c.CountConversion(ctx, "g", "o1", now); err != nil {
			t.Fatal(err)
		}
	}
	if reached, _ := c.OfferCapReached(ctx, "g", "o1", 2, now); !reached {
		t.Error("two purchases against a cap of 2 must be reached")
	}
	// The cap is per offer, per day: another offer and the next day are fresh.
	if reached, _ := c.OfferCapReached(ctx, "g", "o2", 2, now); reached {
		t.Error("another offer must not share the counter")
	}
	if reached, _ := c.OfferCapReached(ctx, "g", "o1", 2, now.Add(24*time.Hour)); reached {
		t.Error("the next day starts from zero")
	}
}

func TestOfferCapIgnoresUnknownOffer(t *testing.T) {
	c, _ := newCounters(t)
	ctx := context.Background()
	if err := c.CountConversion(ctx, "g", "", time.Now()); err != nil {
		t.Errorf("no offer: %v, want nil (nothing to count)", err)
	}
	if reached, err := c.OfferCapReached(ctx, "", "", 1, time.Now()); reached || err != nil {
		t.Errorf("no offer: (%v,%v)", reached, err)
	}
}

func TestOfferClickCapCountsAtomically(t *testing.T) {
	c, mr := newCounters(t)
	defer mr.Close()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	mr.SetTime(now)
	for i := 0; i < 2; i++ {
		reached, err := c.CountClick(context.Background(), "g", "o1", 2, now)
		if err != nil || reached {
			t.Fatalf("click %d: reached=%v err=%v, want served", i+1, reached, err)
		}
	}
	reached, err := c.CountClick(context.Background(), "g", "o1", 2, now)
	if err != nil || !reached {
		t.Fatalf("third click: reached=%v err=%v, want capped", reached, err)
	}
	if !mr.Exists("offerclicks:g:o1:20261021") {
		t.Fatal("click counter key missing")
	}
	// The key expires within a day (miniredis uses the real clock for TTL).
	if ttl := mr.TTL("offerclicks:g:o1:20261021"); ttl <= 0 || ttl > 24*time.Hour {
		t.Fatalf("ttl = %v, want (0, 24h]", ttl)
	}
	// A disabled cap does not touch Redis.
	reached, err = c.CountClick(context.Background(), "g", "o2", 0, now)
	if err != nil || reached {
		t.Fatalf("uncapped: reached=%v err=%v", reached, err)
	}
	if mr.Exists("offerclicks:g:o2:20261021") {
		t.Fatal("uncapped offer must not create a counter")
	}
}

func TestCountConversionOnce(t *testing.T) {
	c, mr := newCounters(t)
	defer mr.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	mr.SetTime(now)

	ok, err := c.CountConversionOnce(ctx, "g", "o1", "c1\x00t1", now)
	if err != nil || !ok {
		t.Fatalf("first: counted=%v err=%v, want true", ok, err)
	}
	ok, err = c.CountConversionOnce(ctx, "g", "o1", "c1\x00t1", now)
	if err != nil || ok {
		t.Fatalf("duplicate: counted=%v err=%v, want false", ok, err)
	}
	if n, err := mr.Get("offercap:g:o1:20261021"); err != nil || n != "1" {
		t.Fatalf("counter = %q err=%v, want 1", n, err)
	}
	ok, err = c.CountConversionOnce(ctx, "g", "o1", "c1\x00t2", now)
	if err != nil || !ok {
		t.Fatalf("second tid: counted=%v err=%v, want true", ok, err)
	}
	// A retry after midnight is still a duplicate of the same transaction.
	ok, err = c.CountConversionOnce(ctx, "g", "o1", "c1\x00t1", now.Add(24*time.Hour))
	if err != nil || ok {
		t.Fatalf("next-day retry: counted=%v err=%v, want false", ok, err)
	}
	// Missing identifiers are not an error and never count.
	ok, err = c.CountConversionOnce(ctx, "", "", "x", now)
	if err != nil || ok {
		t.Fatalf("empty ids: counted=%v err=%v", ok, err)
	}
}
