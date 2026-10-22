package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/egerkuzma/kuztds/internal/config"
	"github.com/egerkuzma/kuztds/internal/store"
	"github.com/redis/go-redis/v9"
)

type fakePostbackStore struct {
	group, offer string
	recordErr    error
	records      int
	lastCID      string
	lastTID      string
}

func (f *fakePostbackStore) EventGroupID(context.Context, string) (string, error) {
	return f.group, nil
}

func (f *fakePostbackStore) EventOfferID(context.Context, string) (string, error) {
	return f.offer, nil
}

func (f *fakePostbackStore) RecordPostback(_ context.Context, cid string, _ float64, _, tid, _ string, _ float64) error {
	f.records++
	f.lastCID, f.lastTID = cid, tid
	return f.recordErr
}

func dedupeEnv(t *testing.T, st *fakePostbackStore) (httpHandler, *store.Counters) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	c := store.NewCounters(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	groups := config.NewGroups(&config.Group{
		ID: "g", Status: true, Redirect: "stop",
		Offers: []config.Offer{{ID: "main", URL: "https://main.example", Status: true, CapDaily: 10}},
	})
	h := testEnv(t, groups, func(d *engineDeps) {
		d.pbStore = st
		d.counters = c
		d.postbackKey = "secret"
	})
	return h, c
}

type httpHandler = interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

func postback(t *testing.T, h httpHandler, query string) {
	t.Helper()
	do(t, h, "/?pb=secret&"+query, "8.8.8.8", nil)
}

func capReached(t *testing.T, c *store.Counters) bool {
	t.Helper()
	reached, err := c.OfferCapReached(context.Background(), "g", "main", 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return reached
}

func TestPostbackFailedRecordDoesNotSpendCap(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main", recordErr: errors.New("clickhouse down")}
	h, c := dedupeEnv(t, st)
	postback(t, h, "cid=c1&tid=t1&status=sale&profit=5")
	if st.records != 1 {
		t.Fatalf("records = %d, want 1", st.records)
	}
	if capReached(t, c) {
		t.Fatal("a failed insert must not spend a purchase slot")
	}
}

func TestPostbackDuplicateDoesNotSpendSecondSlot(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, c := dedupeEnv(t, st)
	postback(t, h, "cid=c1&tid=t1&status=sale&profit=5")
	postback(t, h, "cid=c1&tid=t1&status=sale&profit=5")
	if st.records != 2 {
		t.Fatalf("records = %d, want 2 (store dedupes)", st.records)
	}
	reached, err := c.OfferCapReached(context.Background(), "g", "main", 2, time.Now())
	if err != nil || reached {
		t.Fatalf("one unique conversion against cap 2: reached=%v err=%v", reached, err)
	}
}

func TestPostbackDistinctTIDsSpendTwoSlots(t *testing.T) {
	st := &fakePostbackStore{group: "g", offer: "main"}
	h, c := dedupeEnv(t, st)
	postback(t, h, "cid=c1&tid=t1&status=sale&profit=5")
	postback(t, h, "cid=c1&tid=t2&status=sale&profit=5")
	reached, err := c.OfferCapReached(context.Background(), "g", "main", 2, time.Now())
	if err != nil || !reached {
		t.Fatalf("two distinct transactions against cap 2: reached=%v err=%v", reached, err)
	}
}
