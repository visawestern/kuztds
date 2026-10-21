package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egerkuzma/kuztds/internal/config"
)

// notFoundStore holds the operator's custom 404 page (G-132), re-read on the
// same tick as the groups config. The page is optional: without it trash mode
// 3 keeps the built-in http.NotFound answer.
type notFoundStore struct {
	path  string
	log   *slog.Logger
	stamp fileStamp
	v     atomic.Pointer[[]byte]
}

func newNotFoundStore(path string, log *slog.Logger) *notFoundStore {
	if log == nil {
		log = slog.Default()
	}
	s := &notFoundStore{path: path, log: log}
	if _, err := s.reload(); err != nil {
		log.Warn("404 page not loaded, the built-in 404 stays", "path", path, "err", err)
	}
	return s
}

// Get returns the current page. ok=false when the file is absent or blank.
func (s *notFoundStore) Get() ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	if p := s.v.Load(); p != nil {
		return *p, true
	}
	return nil, false
}

func (s *notFoundStore) reload() (bool, error) {
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
	if st.size > config.MaxNotFoundBytes {
		return false, fmt.Errorf("404 page is larger than %d bytes", config.MaxNotFoundBytes)
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(b)) == "" {
		s.v.Store(nil)
	} else {
		page := b
		s.v.Store(&page)
	}
	s.stamp = *st
	s.log.Info("404 page loaded", "path", s.path, "bytes", len(b))
	return true, nil
}

func (s *notFoundStore) watch(ctx context.Context, interval time.Duration) {
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
				s.log.Warn("404 page reload failed, keeping the previous page", "path", s.path, "err", err)
			}
		}
	}
}
