package main

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egerkuzma/kuztds/internal/store"
)

type fakePBStore struct {
	mu      sync.Mutex
	batches [][]store.PostbackError
}

func (f *fakePBStore) InsertPostbackErrors(_ context.Context, rows []store.PostbackError) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, append([]store.PostbackError(nil), rows...))
	return nil
}

func (f *fakePBStore) all() []store.PostbackError {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.PostbackError
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

func TestPBErrorLogBatchesAndDrainsOnClose(t *testing.T) {
	fs := &fakePBStore{}
	q := newPBErrorLog(fs, slog.New(slog.NewTextHandler(io.Discard, nil)), 2, time.Hour)
	go q.Run()
	for i := 0; i < 3; i++ {
		q.Push(store.PostbackError{Reason: "status_invalid", CID: string(rune('a' + i))})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(fs.all()); got != 3 {
		t.Fatalf("rows = %d, want 3", got)
	}
	if len(fs.batches) < 2 {
		t.Fatalf("expected a full batch and a final flush, got %d batches", len(fs.batches))
	}
}

func TestPBErrorLogDropsWhenFull(t *testing.T) {
	q := newPBErrorLog(&fakePBStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)), 10, time.Hour)
	for i := 0; i < cap(q.in)+1; i++ {
		q.Push(store.PostbackError{Reason: "one"})
	}
	if q.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", q.Dropped())
	}
}

type fakeJournal struct{ rows []store.PostbackError }

func (f *fakeJournal) Push(e store.PostbackError) { f.rows = append(f.rows, e) }

func TestPostbackRejectReason(t *testing.T) {
	cases := []struct{ q, want string }{
		{"status=sale", "cid_missing"},
		{"cid=abc", "status_missing"},
		{"cid=abc&status=not/a/status", "status_invalid"},
		{"cid=abc&status=SALE", ""},
	}
	for _, c := range cases {
		v, _ := url.ParseQuery(c.q)
		if got := postbackRejectReason(v); got != c.want {
			t.Errorf("%s → %q, want %q", c.q, got, c.want)
		}
	}
}

func TestRedactedPostbackQuery(t *testing.T) {
	v, _ := url.ParseQuery("pb=secret&cid=abc&status=sale&tid=42")
	got := redactedPostbackQuery(v)
	if strings.Contains(got, "secret") || strings.Contains(got, "pb=") {
		t.Fatalf("postback key leaked: %q", got)
	}
	if !strings.Contains(got, "cid=abc") || !strings.Contains(got, "tid=42") {
		t.Fatalf("useful fields missing: %q", got)
	}
	long := "cid=" + strings.Repeat("x", 5000)
	v, _ = url.ParseQuery(long)
	if n := len(redactedPostbackQuery(v)); n > 2048 {
		t.Fatalf("query length = %d, want <= 2048", n)
	}
}

func TestHandlerJournalsRejectedPostback(t *testing.T) {
	j := &fakeJournal{}
	h := testEnv(t, group(), func(d *engineDeps) {
		d.postbackKey = "secret"
		d.pbErrors = j
	})
	rec := do(t, h, "/?pb=secret&cid=abc&status=not/a/status&tid=9&profit=1", "8.8.8.8", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(j.rows) != 1 {
		t.Fatalf("journal rows = %d, want 1", len(j.rows))
	}
	e := j.rows[0]
	if e.Reason != "status_invalid" || e.CID != "abc" || e.Status != "not/a/status" || e.TID != "9" {
		t.Fatalf("entry = %+v", e)
	}
	if e.IP != "8.8.8.8" || strings.Contains(e.Query, "secret") {
		t.Fatalf("ip/query = %q / %q", e.IP, e.Query)
	}
}

func TestJournalQueryDropsUnknownTokens(t *testing.T) {
	q, _ := url.ParseQuery("cid=abc&status=sale&tid=9&profit=1&payout=2&currency=EUR&password=hunter2&token=xyz&pb=secret")
	got := redactedPostbackQuery(q)
	for _, bad := range []string{"hunter2", "xyz", "secret", "password", "token"} {
		if strings.Contains(got, bad) {
			t.Fatalf("journal query leaked %q: %q", bad, got)
		}
	}
	for _, good := range []string{"cid=abc", "status=sale", "tid=9", "profit=1", "payout=2", "currency=EUR"} {
		if !strings.Contains(got, good) {
			t.Fatalf("journal query lost %q: %q", good, got)
		}
	}
}
