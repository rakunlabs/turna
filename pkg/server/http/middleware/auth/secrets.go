package auth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Secret fields are write-only on management GETs. The trusted runtime
// session-provider export is intentionally separate and still supplies secrets.
func secretField(key string) bool {
	switch strings.ToLower(key) {
	case "password", "client_secret", "private_key", "secret", "registration_token":
		return true
	}
	return false
}

func redactSecrets(raw []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	redactSecretValue(value)
	return json.Marshal(value)
}

func redactSecretValue(value any) {
	switch node := value.(type) {
	case map[string]any:
		for key, value := range node {
			text, isString := value.(string)
			if secretField(key) && isString {
				node[key+"_set"] = text != ""
				node[key] = ""
			} else {
				redactSecretValue(value)
			}
		}
	case []any:
		for _, item := range node {
			redactSecretValue(item)
		}
	}
}

// mergeSecrets keeps stored secrets when omitted or blank. An explicit null
// clears a secret; flags returned by GET are presentation-only and discarded.
// Dynamic child records that were removed are never restored.
func mergeSecrets(incoming, stored []byte) ([]byte, error) {
	var next, old map[string]any
	if err := json.Unmarshal(incoming, &next); err != nil {
		return nil, err
	}
	if next == nil {
		return nil, fmt.Errorf("configuration must be a JSON object")
	}
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &old); err != nil {
			return nil, err
		}
	}
	mergeSecretMap(next, old)
	return json.Marshal(next)
}

func mergeSecretMap(next, old map[string]any) {
	for key, value := range next {
		if strings.HasSuffix(key, "_set") && secretField(strings.TrimSuffix(key, "_set")) {
			delete(next, key)
			continue
		}
		if secretField(key) && value == nil {
			continue
		}
		if child, ok := value.(map[string]any); ok {
			previous, _ := old[key].(map[string]any)
			mergeSecretMap(child, previous)
		}
	}
	for key, value := range old {
		if !secretField(key) {
			continue
		}
		// Arbitrary maps (e.g. role_map) can have a key named "password"
		// whose value is not a credential. Do not reinterpret such entries.
		if _, ok := value.(string); !ok {
			continue
		}
		current, exists := next[key]
		if !exists || current == "" {
			next[key] = value
		}
	}
	for key, value := range next {
		if secretField(key) && value == nil {
			next[key] = ""
		}
	}
}
