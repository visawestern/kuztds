package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egerkuzma/kuztds/internal/config"
)

// verificationFile is one root-level file an ad cabinet fetches to verify the
// domain (G-060). The body and type are computed once on load: serving it is a
// map lookup, not a disk read on the hot path.
type verificationFile struct {
	body []byte
	ct   string
}

// maxVerificationDocBytes bounds the whole document. The admin caps its save
// at 1 MiB; a file edited by hand must not make the engine load a huge blob.
const maxVerificationDocBytes = 4 << 20

// verificationStore holds the files in an immutable snapshot, re-read on the
// same tick as the groups config.
type verificationStore struct {
	path  string
	log   *slog.Logger
	stamp fileStamp
	v     atomic.Pointer[map[string]verificationFile]
}

func newVerificationStore(path string, log *slog.Logger) *verificationStore {
	if log == nil {
		log = slog.Default()
	}
	s := &verificationStore{path: path, log: log}
	if _, err := s.reload(); err != nil {
		log.Warn("verification file not loaded", "path", path, "err", err)
	}
	return s
}

// Get returns a published file by its exact root name.
func (s *verificationStore) Get(name string) (verificationFile, bool) {
	if s == nil {
		return verificationFile{}, false
	}
	if p := s.v.Load(); p != nil {
		f, ok := (*p)[name]
		return f, ok
	}
	return verificationFile{}, false
}

// reload re-reads the document when mtime or size changed. A missing file is
// not an error — verification is optional. A corrupt one keeps the previous
// snapshot: the domain must not lose verification because of a torn write.
func (s *verificationStore) reload() (bool, error) {
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
	if st.size > maxVerificationDocBytes {
		return false, fmt.Errorf("verification document is larger than %d bytes", maxVerificationDocBytes)
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return false, err
	}
	var doc config.Verifications
	if err := json.Unmarshal(b, &doc); err != nil {
		return false, err
	}
	files := make(map[string]verificationFile, len(doc.Files))
	for _, f := range doc.Files {
		name := strings.TrimSpace(f.Name)
		content := []byte(f.Content)
		if !config.ValidVerificationName(name) || strings.TrimSpace(f.Content) == "" || len(content) > config.MaxVerificationBytes {
			s.log.Warn("verification file skipped", "name", f.Name)
			continue
		}
		files[name] = verificationFile{body: content, ct: verificationContentType(name)}
	}
	s.v.Store(&files)
	s.stamp = *st
	s.log.Info("verification files loaded", "path", s.path, "count", len(files))
	return true, nil
}

func (s *verificationStore) watch(ctx context.Context, interval time.Duration) {
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
				s.log.Warn("verification reload failed, keeping the previous files", "err", err)
			}
		}
	}
}

// verificationContentType picks a type ad cabinets accept for their files.
func verificationContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
