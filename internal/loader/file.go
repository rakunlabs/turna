package loader

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

const (
	defaultFilePerm   fs.FileMode = 0o644
	defaultFolderPerm fs.FileMode = 0o755
)

// loadFileRaw reads a file and returns its raw content.
func loadFileRaw(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return b, nil
}

// loadFileMap reads a file and decodes it into a map based on its extension.
func loadFileMap(path string, v any) error {
	c, err := codecByExt(filepath.Ext(path))
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer f.Close()

	if err := c.Decode(f, v); err != nil {
		return fmt.Errorf("failed to decode file %s: %w", path, err)
	}

	return nil
}

// writeRaw writes raw bytes to path, creating parent folders as needed.
func writeRaw(path string, data []byte, filePerm, folderPerm string) error {
	f, err := openForWrite(path, filePerm, folderPerm)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// writeCodec writes data to path encoded by the file extension's codec.
func writeCodec(path string, data any, filePerm, folderPerm string) error {
	c, err := codecByExt(filepath.Ext(path))
	if err != nil {
		return err
	}

	f, err := openForWrite(path, filePerm, folderPerm)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := c.Encode(f, data); err != nil {
		return fmt.Errorf("failed to encode file %s: %w", path, err)
	}

	return nil
}

func openForWrite(path, filePerm, folderPerm string) (*os.File, error) {
	folder := filepath.Dir(path)
	if _, err := os.Stat(folder); os.IsNotExist(err) {
		perm, err := parsePerm(folderPerm, defaultFolderPerm)
		if err != nil {
			return nil, err
		}

		if err := os.MkdirAll(folder, perm); err != nil {
			return nil, fmt.Errorf("failed to create folder %s: %w", folder, err)
		}
	}

	perm, err := parsePerm(filePerm, defaultFilePerm)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}

	return f, nil
}

// parsePerm parses an octal permission string such as "0644".
// An empty string returns the provided default.
func parsePerm(perm string, def fs.FileMode) (fs.FileMode, error) {
	if perm == "" {
		return def, nil
	}

	v, err := strconv.ParseUint(perm, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid permission %q: %w", perm, err)
	}

	return fs.FileMode(v), nil
}
