package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egerkuzma/kuztds/internal/config"
)

// G-060: the store keeps a snapshot, skips names the engine must not publish
// and keeps the previous revision when the file is corrupt.
func TestVerificationStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.json")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	missing := newVerificationStore(filepath.Join(dir, "nope.json"), nil)
	if _, ok := missing.Get("a.html"); ok {
		t.Error("a missing file published something")
	}
	var none *verificationStore
	if _, ok := none.Get("a.html"); ok {
		t.Error("a nil store published something")
	}
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := newVerificationStore(broken, nil).Get("a.html"); ok {
		t.Error("a corrupt initial document published something")
	}

	write(`{"files":[
		{"name":"ok.html","content":"<meta x>"},
		{"name":"note.xml","content":"<x/>"},
		{"name":"healthz","content":"no"},
		{"name":"../evil","content":"no"},
		{"name":"empty.txt","content":"  "}
	]}`)
	s := newVerificationStore(path, nil)
	if f, ok := s.Get("ok.html"); !ok || string(f.body) != "<meta x>" || f.ct != "text/html; charset=utf-8" {
		t.Errorf("ok.html = (%q,%q,%v)", f.body, f.ct, ok)
	}
	if f, ok := s.Get("note.xml"); !ok || f.ct != "application/xml; charset=utf-8" {
		t.Errorf("note.xml = (%q,%q)", f.body, f.ct)
	}
	for _, name := range []string{"healthz", "../evil", "empty.txt"} {
		if _, ok := s.Get(name); ok {
			t.Errorf("%q must not be published", name)
		}
	}

	// A newer, broken document must not drop yesterday's working files.
	write(`{"files":`)
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reload(); err == nil {
		t.Fatal("a corrupt document must return an error")
	}
	if _, ok := s.Get("ok.html"); !ok {
		t.Error("a corrupt reload dropped the previous snapshot")
	}

	// A valid replacement is published whole.
	write(`{"files":[{"name":"new.txt","content":"done"}]}`)
	future = future.Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.reload(); err != nil || !changed {
		t.Fatalf("reload = (%v,%v)", changed, err)
	}
	if _, ok := s.Get("ok.html"); ok {
		t.Error("old file survived a full replacement")
	}
	if f, ok := s.Get("new.txt"); !ok || string(f.body) != "done" || !strings.HasPrefix(f.ct, "text/plain") {
		t.Errorf("new.txt = (%q,%q,%v)", f.body, f.ct, ok)
	}
}

// A hand-edited document may exceed the per-file bound; the engine must skip
// the file rather than publish a 64 KiB+ page from the tracker root.
func TestVerificationStoreSkipsOversize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.json")
	big := strings.Repeat("x", config.MaxVerificationBytes+1)
	if err := os.WriteFile(path, []byte(`{"files":[{"name":"big.txt","content":"`+big+`"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newVerificationStore(path, nil)
	if _, ok := s.Get("big.txt"); ok {
		t.Error("an oversized file was published")
	}
}

// The document itself is bounded before it is read into memory.
func TestVerificationStoreSkipsHugeDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxVerificationDocBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := newVerificationStore(path, nil).reload(); err == nil {
		t.Fatal("a document above the cap must be refused")
	}
}

// The watcher publishes a new revision on its tick.
func TestVerificationStoreWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"files":[{"name":"a.txt","content":"`+body+`"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	s := newVerificationStore(path, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.watch(ctx, time.Millisecond)
	write("two")
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if f, ok := s.Get("a.txt"); ok && string(f.body) == "two" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watcher did not publish the new revision")
		}
		time.Sleep(time.Millisecond)
	}
	// A broken edit on a tick is logged and the previous revision stays.
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	future = future.Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if f, ok := s.Get("a.txt"); !ok || string(f.body) != "two" {
		t.Errorf("a corrupt tick dropped the working revision: (%q,%v)", f.body, ok)
	}
}

// A path that cannot be read is reported, and a cancelled watcher exits.
func TestVerificationStoreErrorsAndStop(t *testing.T) {
	dir := t.TempDir()
	if _, err := newVerificationStore(dir, nil).reload(); err == nil {
		t.Fatal("a directory is not a verification document")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		newVerificationStore(filepath.Join(dir, "none.json"), nil).watch(ctx, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the watcher ignored a cancelled context")
	}
}

// A file inside a directory we may not traverse is a stat error, not a
// missing file: it must be reported rather than silently treated as empty.
func TestVerificationStoreUnreadablePath(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "verify.json")
	if err := os.WriteFile(path, []byte(`{"files":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	if _, err := newVerificationStore(path, nil).reload(); err == nil {
		t.Fatal("an unreadable path must return an error")
	}
}
