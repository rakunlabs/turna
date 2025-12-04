package replace

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"github.com/rakunlabs/turna/pkg/render"
)

type Config struct {
	Path string `cfg:"path"`
	// SkipFiles is the files to skip, use glob pattern.
	SkipFiles []string `cfg:"skip_files"`
	// SkipDirs is the dirs to skip.
	SkipDirs []string  `cfg:"skip_dirs"`
	Contents []Content `cfg:"contents"`
}

type Content struct {
	checked bool
	reg     *regexp.Regexp
	// Regex is the regex to match the content.
	//  - If regex is not empty, Old will be ignored.
	Regex string `cfg:"regex"`
	// Old is the old content to replace.
	Old string `cfg:"old"`
	old []byte
	New string `cfg:"new"`
	new []byte

	// Value from load name, key value and type is map[string]interface{}
	Value      string `cfg:"value"`
	valueBytes []oldNew
}

func (c *Content) Check(v bool) {
	c.checked = v
}

func (c *Content) set() error {
	if c.checked {
		return nil
	}

	if c.Regex != "" {
		var err error
		c.reg, err = regexp.Compile(c.Regex)
		if err != nil {
			return err
		}
	}

	c.old = []byte(c.Old)
	c.new = []byte(c.New)

	if c.Value != "" {
		v, ok := render.Data[c.Value].(map[string]interface{})
		if !ok {
			return fmt.Errorf("inject value %s is not map[string]interface{}", c.Value)
		}

		valuesOldNew := make([]oldNew, 0, len(v))
		for k, v := range v {
			valuesOldNew = append(valuesOldNew, oldNew{
				Old: []byte(k),
				New: []byte(fmt.Sprintf("%v", v)),
			})
		}

		c.valueBytes = valuesOldNew
	}

	c.checked = true

	return nil
}

type oldNew struct {
	Old []byte `cfg:"old"`
	New []byte `cfg:"new"`
}

func (c *Config) Run(ctx context.Context) error {
	skipFilesMap := make(map[string]struct{}, len(c.SkipFiles))
	for _, dir := range c.SkipFiles {
		skipFilesMap[dir] = struct{}{}
	}

	skipDirsMap := make(map[string]struct{}, len(c.SkipDirs))
	for _, dir := range c.SkipDirs {
		skipDirsMap[dir] = struct{}{}
	}

	if err := filepath.Walk(c.Path, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failure accessing a path %q: %w", path, err)
		}

		if info.IsDir() {
			if _, ok := skipDirsMap[path]; ok {
				slog.Debug("skip dir", "dir", path)

				return filepath.SkipDir
			}

			return nil
		}

		if _, ok := skipFilesMap[path]; ok {
			slog.Debug("skip file", "file", path)
		}

		for i := range c.Contents {
			if err := c.Contents[i].set(); err != nil {
				return err
			}

			if err := replace(path, c.Contents[i]); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to walking the path: %w", err)
	}

	return nil
}

func replace(file string, content Content) error {
	// Read file
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	if content.valueBytes != nil {
		for _, value := range content.valueBytes {
			b = bytes.ReplaceAll(b, value.Old, value.New)
		}
	} else {
		// Replace old content with new content
		if content.reg != nil {
			b = content.reg.ReplaceAll(b, content.new)
		} else {
			b = bytes.ReplaceAll(b, content.old, content.new)
		}
	}

	// Write to file
	return os.WriteFile(file, b, 0)
}
