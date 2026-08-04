package store

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// Compatibility modes for the session storage format.
//
// Turna v0.8.x stored the raw session ID in an unsigned cookie and the session
// values as gob. The current format signs the cookie and stores the values as
// JSON. The two are not interchangeable, so a deployment that runs both
// versions behind the same cookie name and key prefix needs CompatV1 until the
// older version is retired.
const (
	// CompatNone is the current format: signed cookie, JSON payload.
	CompatNone = ""
	// CompatV1 is the v0.8.x format: unsigned cookie holding the raw session
	// ID, gob payload. Use it while an older turna shares the same cookie.
	CompatV1 = "v1"
	// CompatMixed reads both formats but writes the current one. Use it after
	// the older turna is retired to migrate sessions without logging users out.
	CompatMixed = "mixed"
)

// ValidCompat reports whether mode is a supported compatibility mode.
func ValidCompat(mode string) bool {
	switch mode {
	case CompatNone, CompatV1, CompatMixed:
		return true
	default:
		return false
	}
}

// encodeLegacyValues serializes session values the way the v0.8.x store did:
// gob over a map[any]any. gob registers string and the other basic types by
// default, so turna's values need no gob.Register call.
func encodeLegacyValues(values map[string]any) ([]byte, error) {
	legacy := make(map[any]any, len(values))
	for k, v := range values {
		legacy[k] = v
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(legacy); err != nil {
		return nil, fmt.Errorf("encode legacy session values: %w", err)
	}

	return buf.Bytes(), nil
}

// decodeLegacyValues parses a gob payload written by the v0.8.x store. Entries
// with a non-string key cannot be represented in the current session, so they
// are skipped instead of failing the whole session.
func decodeLegacyValues(data []byte) (map[string]any, error) {
	legacy := make(map[any]any)
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&legacy); err != nil {
		return nil, fmt.Errorf("decode legacy session values: %w", err)
	}

	values := make(map[string]any, len(legacy))
	for k, v := range legacy {
		key, ok := k.(string)
		if !ok {
			continue
		}

		values[key] = v
	}

	return values, nil
}

// decodeJSONValues parses a payload in the current JSON format.
func decodeJSONValues(data []byte) (map[string]any, error) {
	values := make(map[string]any)
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}

	return values, nil
}

// decodeAnyValues parses a payload in the current JSON format and falls back to
// the legacy gob format. A gob payload is binary and never parses as a JSON
// object, so the fallback is unambiguous.
func decodeAnyValues(data []byte) (map[string]any, error) {
	if values, err := decodeJSONValues(data); err == nil {
		return values, nil
	}

	return decodeLegacyValues(data)
}
