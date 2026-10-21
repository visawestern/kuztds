package store

import (
	"context"
	"testing"
	"time"
)

// G-050: purchases per offer per UTC day. The click path reads the counter;
// the postback writes it.

func TestOfferCapReached(t *testing.T) {
	c, _ := newCounters(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

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
