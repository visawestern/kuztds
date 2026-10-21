package admin

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/egerkuzma/kuztds/internal/config"
)

// FileVerifications stores root verification files in one JSON document. The
// engine reads the same file (KUZTDS_VERIFY_FILE), so a save goes live on its
// next reload tick without touching the engine process.
type FileVerifications struct {
	path string
}

func NewFileVerifications(path string) *FileVerifications { return &FileVerifications{path: path} }

func (f *FileVerifications) List(context.Context) (config.Verifications, error) {
	b, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.Verifications{Files: []config.Verification{}}, nil
		}
		return config.Verifications{}, err
	}
	var doc config.Verifications
	if err := json.Unmarshal(b, &doc); err != nil {
		return config.Verifications{}, err
	}
	if doc.Files == nil {
		doc.Files = []config.Verification{}
	}
	return doc, nil
}

func (f *FileVerifications) Save(_ context.Context, doc config.Verifications) error {
	if doc.Files == nil {
		doc.Files = []config.Verification{}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(f.path, b, 0o644)
}
