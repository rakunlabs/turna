package config

import (
	"os"
	"testing"

	"github.com/rakunlabs/turna/pkg/render"
)

func TestApplyEnv(t *testing.T) {
	previous := render.Data
	t.Cleanup(func() { render.Data = previous })
	render.Data = map[string]any{
		"first":  map[string]any{"TURNA_TEST_ENV": "first", "TURNA_TEST_EXTRA": "{{ .value }}"},
		"second": map[string]any{"TURNA_TEST_ENV": "second"},
		"value":  "loaded",
	}
	t.Setenv("TURNA_TEST_ENV", "original")
	t.Setenv("TURNA_TEST_EXTRA", "original")
	t.Setenv("TURNA_TEST_UNRELATED", "unchanged")
	t.Setenv("TURNA_TEST_NEW", "")
	if err := os.Unsetenv("TURNA_TEST_NEW"); err != nil {
		t.Fatal(err)
	}

	if err := ApplyEnv(nil, []string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TURNA_TEST_ENV"); got != "second" {
		t.Fatalf("path precedence: got %q", got)
	}
	if got := os.Getenv("TURNA_TEST_EXTRA"); got != "loaded" {
		t.Fatalf("loaded template: got %q", got)
	}
	if err := ApplyEnv(map[string]any{"TURNA_TEST_ENV": "{{ .value }}", "TURNA_TEST_EXTRA": "", "TURNA_TEST_NEW": 42}, []string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TURNA_TEST_ENV"); got != "loaded" {
		t.Fatalf("explicit env precedence: got %q", got)
	}
	if got, ok := os.LookupEnv("TURNA_TEST_EXTRA"); !ok || got != "" {
		t.Fatalf("empty env should remain set: got %q, present %v", got, ok)
	}
	if got := os.Getenv("TURNA_TEST_UNRELATED"); got != "unchanged" {
		t.Fatalf("unrelated env changed: %q", got)
	}
	if got := os.Getenv("TURNA_TEST_NEW"); got != "42" {
		t.Fatalf("new env: got %q", got)
	}
}

func TestApplyEnvErrorsDoNotChangeEnvironment(t *testing.T) {
	previous := render.Data
	t.Cleanup(func() { render.Data = previous })
	render.Data = map[string]any{"not_map": "text"}
	for _, tc := range []struct {
		name  string
		env   map[string]any
		paths []string
	}{
		{name: "missing path", paths: []string{"missing"}},
		{name: "non-map path", paths: []string{"not_map"}},
		{name: "empty key", env: map[string]any{"": "value"}},
		{name: "equals in key", env: map[string]any{"BAD=KEY": "value"}},
		{name: "NUL in key", env: map[string]any{"BAD\x00KEY": "value"}},
		{name: "NUL in value", env: map[string]any{"ZZ_BAD": "secret\x00value"}},
		{name: "invalid template", env: map[string]any{"ZZ_BAD": "{{"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AA_TURNA_TEST_ENV", "original")
			values := map[string]any{"AA_TURNA_TEST_ENV": "changed"}
			for key, value := range tc.env {
				values[key] = value
			}
			if err := ApplyEnv(values, tc.paths); err == nil {
				t.Fatal("expected error")
			}
			if got := os.Getenv("AA_TURNA_TEST_ENV"); got != "original" {
				t.Fatalf("environment changed on failure: %q", got)
			}
		})
	}
}
