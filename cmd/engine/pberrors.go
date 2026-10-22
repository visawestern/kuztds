package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egerkuzma/kuztds/internal/conversion"
	"github.com/egerkuzma/kuztds/internal/server"
	"github.com/egerkuzma/kuztds/internal/store"
)

// pbErrorInserter stores a batch of rejected postbacks.
type pbErrorInserter interface {
	InsertPostbackErrors(ctx context.Context, rows []store.PostbackError) error
}

// pbErrorLog is a small bounded queue in front of the journal. Invalid
// postbacks are diagnostic: Push never waits, and when the queue is full the
// entry is dropped and counted. One writer drains it in batches.
type pbErrorLog struct {
	in      chan store.PostbackError
	ins     pbErrorInserter
	log     *slog.Logger
	batch   int
	every   time.Duration
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
	dropped atomic.Int64
}

func newPBErrorLog(ins pbErrorInserter, log *slog.Logger, batch int, every time.Duration) *pbErrorLog {
	if batch <= 0 {
		batch = 50
	}
	if every <= 0 {
		every = 2 * time.Second
	}
	return &pbErrorLog{
		in:    make(chan store.PostbackError, 512),
		ins:   ins,
		log:   log,
		batch: batch,
		every: every,
		done:  make(chan struct{}),
	}
}

// Push queues one rejected postback without blocking the request.
func (l *pbErrorLog) Push(e store.PostbackError) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		l.dropped.Add(1)
		return
	}
	select {
	case l.in <- e:
	default:
		l.dropped.Add(1)
	}
}

// Dropped counts entries that never reached the queue (full or after close).
func (l *pbErrorLog) Dropped() int64 { return l.dropped.Load() }

// Run drains the queue until Close. A failed insert is logged and the batch is
// gone: the journal must not become a second source of pressure on a broken
// ClickHouse.
func (l *pbErrorLog) Run() {
	defer close(l.done)
	ticker := time.NewTicker(l.every)
	defer ticker.Stop()
	buf := make([]store.PostbackError, 0, l.batch)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := l.ins.InsertPostbackErrors(ctx, buf)
		cancel()
		if err != nil {
			l.log.Warn("postback journal: insert failed", "rows", len(buf), "err", err)
		}
		buf = buf[:0]
	}
	for {
		select {
		case e, ok := <-l.in:
			if !ok {
				flush()
				return
			}
			buf = append(buf, e)
			if len(buf) >= l.batch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Close stops accepting entries, drains the queue and waits for the writer.
func (l *pbErrorLog) Close(ctx context.Context) error {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.in)
	}
	l.mu.Unlock()
	select {
	case <-l.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// postbackJournal accepts rejected postbacks. *pbErrorLog implements it in
// production; tests use a plain recorder.
type postbackJournal interface {
	Push(store.PostbackError)
}

// postbackRejectReason explains why parsePostback refused the query.
func postbackRejectReason(q url.Values) string {
	if q.Get("cid") == "" {
		return "cid_missing"
	}
	if q.Get("status") == "" {
		return "status_missing"
	}
	if _, ok := conversion.ParseStatus(q.Get("status")); !ok {
		return "status_invalid"
	}
	return ""
}

// journalPostback records a rejected postback without blocking the response.
func (d *engineDeps) journalPostback(r *http.Request, reason, detail string) {
	if d.pbErrors == nil {
		return
	}
	q := r.URL.Query()
	ip := ""
	if a := server.ClientIP(r.Context()); a.IsValid() {
		ip = a.String()
	}
	d.pbErrors.Push(store.PostbackError{
		TS:     time.Now(),
		Reason: reason,
		CID:    clip(q.Get("cid"), 256),
		Status: clip(q.Get("status"), 256),
		TID:    clip(q.Get("tid"), 256),
		IP:     ip,
		Query:  redactedPostbackQuery(q),
		Detail: clip(detail, 512),
	})
}

func clip(s string, n int) string {
	if len(s) > n {
		return strings.ToValidUTF8(s[:n], "?")
	}
	return s
}

// journalQuery keeps only the fields the postback parser understands. The
// allowlist is deliberate: an unknown parameter may be a token from a network
// and must not end up in a diagnostic table.
func redactedPostbackQuery(q url.Values) string {
	allowed := [...]string{"cid", "status", "tid", "profit"}
	c := make(url.Values, len(allowed))
	for _, k := range allowed {
		if v, ok := q[k]; ok {
			c[k] = v
		}
	}
	s := c.Encode()
	if len(s) > 2048 {
		s = s[:2048]
	}
	return strings.ToValidUTF8(s, "?")
}
