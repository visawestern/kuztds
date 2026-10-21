package admin

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/egerkuzma/kuztds/internal/config"
)

// FileNotFound stores the custom 404 page (G-132) in a plain HTML file. The
// engine reads the same path (KUZTDS_404_FILE) on its reload tick.
type FileNotFound struct {
	path string
}

func NewFileNotFound(path string) *FileNotFound { return &FileNotFound{path: path} }

func (f *FileNotFound) Get(context.Context) (string, error) {
	st, err := os.Stat(f.path)
	if err == nil && st.Size() > config.MaxNotFoundBytes {
		return "", fmt.Errorf("404 page is larger than %d bytes", config.MaxNotFoundBytes)
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

func (f *FileNotFound) Save(_ context.Context, html string) error {
	if len(html) > config.MaxNotFoundBytes {
		return fmt.Errorf("404 page is larger than %d bytes", config.MaxNotFoundBytes)
	}
	return writeFileAtomic(f.path, []byte(html), 0o644)
}
