package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/store"
	"github.com/redis/go-redis/v9"
)

// G-050: when an offer has reached its daily purchase cap, the visit goes to
// its fallback offer; the traffic keeps flowing.

func cappedGroups() (*config.Groups, *config.Group) {
	grp := &config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{
			{ID: "main", URL: "https://main.example", Status: true, CapDaily: 1, Fallback: "backup"},
			{ID: "backup", URL: "https://backup.example", Status: true},
			{ID: "capped-no-fb", URL: "https://solo.example", Status: true, CapDaily: 1},
		},
		Streams: []config.Stream{
			{Name: "main", Status: true, Out: config.Output{Redirect: "offer", Out: "main"}},
			{Name: "solo", Status: true, Out: config.Output{Redirect: "offer", Out: "capped-no-fb"}},
		},
	}
	return config.NewGroups(grp), grp
}

func newOfferCounters(t *testing.T) (*store.Counters, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return store.NewCounters(rdb), mr
}

func TestOfferCapServesFallback(t *testing.T) {
	// Seed the counter straight through the store: one purchase today.
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	c := store.NewCounters(rdb)
	if err := c.CountConversion(context.Background(), "g", "main", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	groups, _ := cappedGroups()
	h, _, stop := curlEnvWith(t, groups, "0", func(d *engineDeps) { d.counters = c })
	defer stop()
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if rec.Code != 302 || !strings.HasPrefix(rec.Header().Get("Location"), "https://backup.example") {
		t.Errorf("capped offer: %d %q, want the fallback", rec.Code, rec.Header().Get("Location"))
	}
}

func TestOfferCapNotReachedServesMain(t *testing.T) {
	c, mr := newOfferCounters(t)
	defer mr.Close()
	groups, _ := cappedGroups()
	h, _, stop := curlEnvWith(t, groups, "0", func(d *engineDeps) { d.counters = c })
	defer stop()
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if rec.Code != 302 || !strings.HasPrefix(rec.Header().Get("Location"), "https://main.example") {
		t.Errorf("fresh day: %d %q, want the main offer", rec.Code, rec.Header().Get("Location"))
	}
}

func TestOfferCapWithoutFallbackServesTrash(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	c := store.NewCounters(rdb)
	_ = c.CountConversion(context.Background(), "g", "capped-no-fb", time.Now().UTC())

	groups, grp := cappedGroups()
	grp.Streams = grp.Streams[1:] // only the capped offer without a fallback
	h, _, stop := curlEnvWith(t, groups, "0", func(d *engineDeps) { d.counters = c })
	defer stop()
	rec := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if rec.Code != 200 || rec.Header().Get("Location") != "" {
		t.Errorf("cap without fallback: %d %q, want the trash answer", rec.Code, rec.Header().Get("Location"))
	}
}

// G-287: a daily click cap redirects to the offer's cap URL, then to the
// fallback offer, and finally to the trash when neither is configured.

func clickCappedGroups(capURL string) *config.Groups {
	return config.NewGroups(
		&config.Group{
			ID: "g", Status: true, Redirect: "stop",
			Offers: []config.Offer{
				{ID: "main", URL: "https://main.example", Status: true, ClickCap: 1, CapURL: capURL, Fallback: "backup"},
				{ID: "backup", URL: "https://backup.example", Status: true},
			},
			Streams: []config.Stream{
				{Name: "main", Status: true, Out: config.Output{Redirect: "offer", Out: "main"}},
			},
		},
		&config.Group{
			ID: "gsolo", Status: true, Redirect: "stop",
			Offers: []config.Offer{{ID: "solo", URL: "https://solo.example", Status: true, ClickCap: 1}},
			Streams: []config.Stream{
				{Name: "solo", Status: true, Out: config.Output{Redirect: "offer", Out: "solo"}},
			},
		})
}

func TestOfferClickCapRedirects(t *testing.T) {
	c, mr := newOfferCounters(t)
	defer mr.Close()
	h, _, stop := curlEnvWith(t, clickCappedGroups("https://capped.example"), "0", func(d *engineDeps) { d.counters = c })
	defer stop()
	first := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if first.Code != 302 || !strings.HasPrefix(first.Header().Get("Location"), "https://main.example") {
		t.Fatalf("first click: %d %q, want the offer", first.Code, first.Header().Get("Location"))
	}
	second := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if second.Code != 302 || second.Header().Get("Location") != "https://capped.example" {
		t.Fatalf("second click: %d %q, want the cap URL", second.Code, second.Header().Get("Location"))
	}
}

func TestOfferClickCapFallsBackWithoutURL(t *testing.T) {
	c, mr := newOfferCounters(t)
	defer mr.Close()
	h, _, stop := curlEnvWith(t, clickCappedGroups(""), "0", func(d *engineDeps) { d.counters = c })
	defer stop()
	_ = do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	second := do(t, h, "/g", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if second.Code != 302 || !strings.HasPrefix(second.Header().Get("Location"), "https://backup.example") {
		t.Fatalf("capped click: %d %q, want the fallback", second.Code, second.Header().Get("Location"))
	}
}

func TestOfferClickCapWithoutFallbackIsTrash(t *testing.T) {
	c, mr := newOfferCounters(t)
	defer mr.Close()
	h, _, stop := curlEnvWith(t, clickCappedGroups(""), "0", func(d *engineDeps) { d.counters = c })
	defer stop()
	_ = do(t, h, "/gsolo", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	second := do(t, h, "/gsolo", "8.8.8.8", map[string]string{"User-Agent": uaWinChrome})
	if second.Code != 200 {
		t.Fatalf("capped standalone offer: %d, want trash 200", second.Code)
	}
}
