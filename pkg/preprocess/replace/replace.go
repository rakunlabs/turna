package replace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/worldline-go/turna/pkg/render"
)

type Config struct {
	Path     string    `cfg:"path"`
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
		v, ok := render.GlobalRender.Data[c.Value].(map[string]interface{})
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
	// Find files matching the glob pattern
	files, err := filepath.Glob(c.Path)
	if err != nil {
		return err
	}

	for _, file := range files {
		for i := range c.Contents {
			if err := c.Contents[i].set(); err != nil {
				return err
			}

			if err := replace(file, c.Contents[i]); err != nil {
				return err
			}
		}
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
