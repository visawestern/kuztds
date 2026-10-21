package admin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egerkuzma/kuztds/internal/config"
)

func TestFileVerifications(t *testing.T) {
	path := filepath.Join(t.TempDir(), "verify.json")
	f := NewFileVerifications(path)
	doc, err := f.List(context.Background())
	if err != nil || doc.Files == nil || len(doc.Files) != 0 {
		t.Fatalf("missing file = (%+v,%v), want an empty document", doc, err)
	}
	if err := f.Save(context.Background(), config.Verifications{Files: []config.Verification{{Name: "a.html", Content: "x"}}}); err != nil {
		t.Fatal(err)
	}
	doc, err = f.List(context.Background())
	if err != nil || len(doc.Files) != 1 || doc.Files[0].Name != "a.html" {
		t.Fatalf("saved document = (%+v,%v)", doc, err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.List(context.Background()); err == nil {
		t.Error("a corrupt document must return an error")
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if doc, err := f.List(context.Background()); err != nil || doc.Files == nil {
		t.Errorf("an empty object = (%+v,%v), want a non-nil empty list", doc, err)
	}
	if err := f.Save(context.Background(), config.Verifications{}); err != nil {
		t.Fatal(err)
	}
	if doc, err := f.List(context.Background()); err != nil || doc.Files == nil {
		t.Errorf("a nil save = (%+v,%v), want a non-nil empty list", doc, err)
	}
	if err := f.Save(context.Background(), config.Verifications{Files: []config.Verification{{Name: "x", Content: "y"}}}); err != nil {
		t.Fatal(err)
	}
	if err := NewFileVerifications(t.TempDir()).Save(context.Background(), config.Verifications{}); err == nil {
		t.Error("saving onto a directory must fail")
	}
}

func TestFileVerificationsUnreadable(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	if _, err := NewFileVerifications(filepath.Join(parent, "verify.json")).List(context.Background()); err == nil {
		t.Error("an unreadable path must return an error, not an empty document")
	}
}
