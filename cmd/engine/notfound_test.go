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

// G-132: the custom 404 page is optional, hot-reloaded and bounded.
func TestNotFoundStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "404.html")
	missing := newNotFoundStore(filepath.Join(dir, "nope.html"), nil)
	if _, ok := missing.Get(); ok {
		t.Error("a missing page was published")
	}
	var none *notFoundStore
	if _, ok := none.Get(); ok {
		t.Error("a nil store published a page")
	}

	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("   \n\t")
	if _, ok := newNotFoundStore(path, nil).Get(); ok {
		t.Error("a blank page was published")
	}
	write("<h1>lost</h1>")
	s := newNotFoundStore(path, nil)
	if body, ok := s.Get(); !ok || string(body) != "<h1>lost</h1>" {
		t.Errorf("page = (%q,%v)", body, ok)
	}

	write(strings.Repeat("x", config.MaxNotFoundBytes+1))
	if _, err := s.reload(); err == nil {
		t.Fatal("an oversized page must be refused")
	}
	if body, ok := s.Get(); !ok || string(body) != "<h1>lost</h1>" {
		t.Error("an oversized page replaced the working one")
	}

	write("<h2>gone</h2>")
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.reload(); err != nil || !changed {
		t.Fatalf("reload = (%v,%v)", changed, err)
	}
	if body, _ := s.Get(); string(body) != "<h2>gone</h2>" {
		t.Errorf("reloaded page = %q", body)
	}

	// Clearing the page returns the built-in 404.
	write("")
	future = future.Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(); ok {
		t.Error("a cleared page is still published")
	}
}

func TestNotFoundStoreWatchAndErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := newNotFoundStore(dir, nil).reload(); err == nil {
		t.Fatal("a directory is not a page")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		newNotFoundStore(filepath.Join(dir, "none.html"), nil).watch(ctx, 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the watcher ignored a cancelled context")
	}

	path := filepath.Join(dir, "404.html")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newNotFoundStore(path, nil)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go s.watch(ctx2, time.Millisecond)
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if body, ok := s.Get(); ok && string(body) == "two" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watcher did not publish the new page")
		}
		time.Sleep(time.Millisecond)
	}
	// A broken path on a tick is logged and the working page stays.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if body, ok := s.Get(); !ok || string(body) != "two" {
		t.Errorf("a broken tick dropped the working page: (%q,%v)", body, ok)
	}
}

func TestNotFoundStoreUnreadablePath(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "404.html")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	if _, err := newNotFoundStore(path, nil).reload(); err == nil {
		t.Fatal("an unreadable path must return an error")
	}
}
