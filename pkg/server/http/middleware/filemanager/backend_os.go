package filemanager

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

// osBackend keeps the files in a local directory; os.Root rejects paths and
// symlinks escaping it.
type osBackend struct {
	root *os.Root
}

func newOSBackend(dir string) (*osBackend, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}

	return &osBackend{root: root}, nil
}

func (b *osBackend) Open(_ context.Context, name string) (io.ReadSeekCloser, fs.FileInfo, error) {
	f, err := b.root.Open(name)
	if err != nil {
		return nil, nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, nil, err
	}

	return f, info, nil
}

func (b *osBackend) Stat(_ context.Context, name string) (fs.FileInfo, error) {
	return b.root.Stat(name)
}

func (b *osBackend) Lstat(_ context.Context, name string) (fs.FileInfo, error) {
	return b.root.Lstat(name)
}

func (b *osBackend) ReadDir(_ context.Context, name string) ([]fs.FileInfo, error) {
	f, err := b.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	dirEntries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	infos := make([]fs.FileInfo, 0, len(dirEntries))
	for _, d := range dirEntries {
		info, err := d.Info()
		if err != nil {
			continue
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			// follow the link only when it stays inside the root
			if target, err := b.root.Stat(path.Join(filepath.ToSlash(name), d.Name())); err == nil {
				info = renamedInfo{FileInfo: target, name: d.Name()}
			}
		}

		infos = append(infos, info)
	}

	return infos, nil
}

func (b *osBackend) WriteFile(_ context.Context, name string, body io.Reader) error {
	dir := path.Dir(name)
	if err := b.root.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := path.Join(dir, "."+path.Base(name)+".tmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))

	f, err := b.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = b.root.Remove(tmp)

		return err
	}

	if err := f.Close(); err != nil {
		_ = b.root.Remove(tmp)

		return err
	}

	if info, err := b.root.Stat(name); err == nil {
		_ = b.root.Chmod(tmp, info.Mode().Perm())
	}

	if err := b.root.Rename(tmp, name); err != nil {
		_ = b.root.Remove(tmp)

		return err
	}

	return nil
}

func (b *osBackend) MkdirAll(_ context.Context, name string) error {
	return b.root.MkdirAll(name, 0o755)
}

func (b *osBackend) RemoveAll(_ context.Context, name string) error {
	return b.root.RemoveAll(name)
}

func (b *osBackend) Rename(_ context.Context, from, to string) error {
	if err := b.root.MkdirAll(path.Dir(to), 0o755); err != nil {
		return err
	}

	return b.root.Rename(from, to)
}

// renamedInfo keeps the link name for a resolved symlink.
type renamedInfo struct {
	fs.FileInfo
	name string
}

func (i renamedInfo) Name() string { return i.name }
