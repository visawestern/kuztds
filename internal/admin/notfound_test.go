package admin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
)

func TestFileNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "404.html")
	f := NewFileNotFound(path)
	if html, err := f.Get(context.Background()); err != nil || html != "" {
		t.Fatalf("missing page = (%q,%v)", html, err)
	}
	if err := f.Save(context.Background(), "<h1>lost</h1>"); err != nil {
		t.Fatal(err)
	}
	if html, err := f.Get(context.Background()); err != nil || html != "<h1>lost</h1>" {
		t.Fatalf("saved page = (%q,%v)", html, err)
	}
	if err := f.Save(context.Background(), strings.Repeat("x", config.MaxNotFoundBytes+1)); err == nil {
		t.Error("an oversized page must be refused")
	}
	if html, _ := f.Get(context.Background()); html != "<h1>lost</h1>" {
		t.Error("a refused save replaced the page")
	}
	if err := f.Save(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if html, _ := f.Get(context.Background()); html != "" {
		t.Errorf("cleared page = %q", html)
	}
	if _, err := NewFileNotFound(t.TempDir()).Get(context.Background()); err == nil {
		t.Error("a directory must not be read as a page")
	}
	big := filepath.Join(t.TempDir(), "big.html")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", config.MaxNotFoundBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileNotFound(big).Get(context.Background()); err == nil {
		t.Error("an oversized page must not be read")
	}
}
