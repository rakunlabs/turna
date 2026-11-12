package filemanager

import (
	"context"
	"io"
	"io/fs"
)

const (
	backendOS  = "os"
	backendGCS = "gcs"
)

// backend stores the managed files. Names are cleaned, slash separated and
// relative to the root, "." is the root itself (see rel).
type backend interface {
	// Open opens a file for reading; a directory is not an error, the info tells.
	Open(ctx context.Context, name string) (io.ReadSeekCloser, fs.FileInfo, error)
	// Stat follows symlinks that stay inside the root.
	Stat(ctx context.Context, name string) (fs.FileInfo, error)
	// Lstat does not follow symlinks.
	Lstat(ctx context.Context, name string) (fs.FileInfo, error)
	// ReadDir lists a directory, symlinks inside the root are resolved.
	ReadDir(ctx context.Context, name string) ([]fs.FileInfo, error)
	// WriteFile replaces the file atomically, readers never see a partial file.
	// Missing parent directories are created.
	WriteFile(ctx context.Context, name string, body io.Reader) error
	MkdirAll(ctx context.Context, name string) error
	RemoveAll(ctx context.Context, name string) error
	// Rename moves a file or a directory, missing parents of to are created.
	Rename(ctx context.Context, from, to string) error
}
