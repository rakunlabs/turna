package waf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
)

// rootFS resolves the embedded CRS files first and falls back to the OS
// filesystem for user directives and data files.
type rootFS struct{}

func (rootFS) Open(name string) (fs.File, error) {
	f, err := coreruleset.FS.Open(name)
	if err == nil {
		return f, nil
	}

	return os.Open(name)
}

func (rootFS) ReadFile(name string) ([]byte, error) {
	b, err := fs.ReadFile(coreruleset.FS, name)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrInvalid) {
		return nil, err
	}

	return os.ReadFile(name)
}

func (rootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(coreruleset.FS, name)
	if err == nil {
		return entries, nil
	}

	return os.ReadDir(name)
}

func (rootFS) Glob(pattern string) ([]string, error) {
	if matches, err := fs.Glob(coreruleset.FS, pattern); err == nil && len(matches) > 0 {
		return matches, nil
	}

	return filepath.Glob(pattern)
}
