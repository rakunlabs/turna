package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rakunlabs/turna/internal/loader"
	"github.com/rakunlabs/turna/pkg/render"
)

// ApplyEnv sets Turna's process environment once, after loads complete.
// Later env_values paths override earlier ones; explicit env wins over all paths.
// All values are rendered and validated before any environment changes are made.
func ApplyEnv(env map[string]any, paths []string) error {
	values := make(map[string]any)
	for _, path := range paths {
		loaded, ok := loader.InnerPath(path, render.Data).(map[string]any)
		if !ok {
			return fmt.Errorf("process env_values path %q must contain a map", path)
		}
		for key, value := range loaded {
			values[key] = value
		}
	}
	for key, value := range env {
		values[key] = value
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rendered := make(map[string]string, len(values))
	for _, key := range keys {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return fmt.Errorf("invalid process environment key %q", key)
		}
		value, err := render.Execute(values[key])
		if err != nil {
			return fmt.Errorf("unable to render process environment key %q: %w", key, err)
		}
		if strings.ContainsRune(string(value), '\x00') {
			return fmt.Errorf("process environment key %q contains a NUL value", key)
		}
		rendered[key] = string(value)
	}

	for _, key := range keys {
		if err := os.Setenv(key, rendered[key]); err != nil {
			return fmt.Errorf("unable to set process environment key %q: %w", key, err)
		}
	}
	return nil
}
