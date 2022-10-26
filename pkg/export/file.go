package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is exporting bytes to file with checking extensions.
func File(path string, v map[string]interface{}) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}

	// get path extension
	ext := getExtension(path)

	// open file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open file error; %w", err)
	}
	defer f.Close()

	// check extension and get the bytes
	var b []byte

	switch ext {
	case "json":
		b, err = json.Marshal(v)
		if err != nil {
			return fmt.Errorf("json marshal error; %w", err)
		}
	case "yaml", "yml":
		b, err = yaml.Marshal(v)
		if err != nil {
			return fmt.Errorf("yaml marshal error; %w", err)
		}
	default:
		return fmt.Errorf("unknown extension: %s", ext)
	}

	// write bytes to file
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("write file error; %w", err)
	}

	return nil
}

// getExtension return extension of path.
func getExtension(path string) string {
	// get path extension
	ext := filepath.Ext(path)
	// remove dot from extension
	ext = strings.TrimPrefix(ext, ".")
	return ext
}
