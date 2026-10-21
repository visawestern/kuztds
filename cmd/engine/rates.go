package main

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/egerkuzma/kuztds/internal/cost"
)

// ratesStore holds the ECB reference rates G-121 in the form the hot path
// needs: an immutable snapshot swapped whole. The cron service writes
// <DataDir>/rates.json; the engine only reads it, re-checking mtime+size on
// the same tick as the groups config.
type ratesStore struct {
	path  string
	log   *slog.Logger
	stamp fileStamp
	v     atomic.Pointer[map[string]float64]
}

func newRatesStore(path string, log *slog.Logger) *ratesStore {
	if log == nil {
		log = slog.Default()
	}
	s := &ratesStore{path: path, log: log}
	if _, err := s.reload(); err != nil {
		log.Warn("rates file not loaded, non-USD prices will log as unknown", "path", path, "err", err)
	}
	return s
}

// Rates returns the current snapshot (nil when no rates have loaded yet).
// The map is never mutated after publication.
func (s *ratesStore) Rates() map[string]float64 {
	if s == nil {
		return nil
	}
	if p := s.v.Load(); p != nil {
		return *p
	}
	return nil
}

// reload re-reads the file when mtime or size changed. A missing file is not
// an error: pricing simply stays USD-only. A corrupt file keeps the previous
// snapshot — yesterday's rates beat none.
func (s *ratesStore) reload() (bool, error) {
	st, err := statFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if st.mod.Equal(s.stamp.mod) && st.size == s.stamp.size {
		return false, nil
	}
	rates, date, err := cost.LoadRates(s.path)
	if err != nil {
		// Do not advance the stamp: a torn read must be retried next tick.
		return false, err
	}
	s.v.Store(&rates)
	s.stamp = *st
	s.log.Info("rates loaded", "path", s.path, "date", date, "currencies", len(rates))
	return true, nil
}

// watch reloads until ctx is cancelled.
func (s *ratesStore) watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.reload(); err != nil {
				s.log.Warn("rates reload failed, keeping the previous rates", "err", err)
			}
		}
	}
}
