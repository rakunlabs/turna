package loader

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// codec encodes/decodes structured content.
type codec interface {
	Encode(w io.Writer, v any) error
	Decode(r io.Reader, v any) error
}

type yamlCodec struct{}

func (yamlCodec) Encode(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	return nil
}

func (yamlCodec) Decode(r io.Reader, v any) error {
	if err := yaml.NewDecoder(r).Decode(v); err != nil {
		return fmt.Errorf("failed to decode YAML: %w", err)
	}

	return nil
}

type jsonCodec struct{}

func (jsonCodec) Encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

func (jsonCodec) Decode(r io.Reader, v any) error {
	if err := json.NewDecoder(r).Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	return nil
}

type tomlCodec struct{}

func (tomlCodec) Encode(w io.Writer, v any) error {
	if err := toml.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("failed to encode TOML: %w", err)
	}

	return nil
}

func (tomlCodec) Decode(r io.Reader, v any) error {
	if _, err := toml.NewDecoder(r).Decode(v); err != nil {
		return fmt.Errorf("failed to decode TOML: %w", err)
	}

	return nil
}

// codecByName returns the codec for a named codec (YAML, JSON, TOML).
// Empty name defaults to YAML.
func codecByName(name string) (codec, error) {
	switch strings.ToUpper(name) {
	case "", "YAML", "YML":
		return yamlCodec{}, nil
	case "JSON":
		return jsonCodec{}, nil
	case "TOML":
		return tomlCodec{}, nil
	default:
		return nil, fmt.Errorf("codec %s not found", name)
	}
}

// codecByExt returns the codec matching a file extension (e.g. ".yaml").
func codecByExt(ext string) (codec, error) {
	switch strings.ToLower(ext) {
	case ".yaml", ".yml":
		return yamlCodec{}, nil
	case ".json":
		return jsonCodec{}, nil
	case ".toml":
		return tomlCodec{}, nil
	default:
		return nil, fmt.Errorf("unsupported file extension %q", ext)
	}
}

// decodeContent decodes raw bytes into v using the named codec.
func decodeContent(name string, data []byte, v any) error {
	c, err := codecByName(name)
	if err != nil {
		return err
	}

	return c.Decode(strings.NewReader(string(data)), v)
}
