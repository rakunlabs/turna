package store

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rakunlabs/ada/utils/securecookie"
	"github.com/rakunlabs/ada/utils/sessions"
)

// legacyPayload builds the exact wire format the v0.8.x store wrote: gob over
// gorilla's map[interface{}]interface{} session values.
func legacyPayload(t *testing.T, values map[any]any) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(values); err != nil {
		t.Fatalf("encode legacy payload: %v", err)
	}

	return buf.Bytes()
}

func TestValidCompat(t *testing.T) {
	for _, mode := range []string{CompatNone, CompatV1, CompatMixed} {
		if !ValidCompat(mode) {
			t.Errorf("ValidCompat(%q) = false, want true", mode)
		}
	}

	for _, mode := range []string{"v2", "legacy", "V1", "true"} {
		if ValidCompat(mode) {
			t.Errorf("ValidCompat(%q) = true, want false", mode)
		}
	}
}

func TestLegacyValuesRoundTrip(t *testing.T) {
	want := map[string]any{
		"token":    "eyJhbGciOiJSUzI1NiJ9",
		"provider": "keycloak",
	}

	data, err := encodeLegacyValues(want)
	if err != nil {
		t.Fatalf("encodeLegacyValues: %v", err)
	}

	got, err := decodeLegacyValues(data)
	if err != nil {
		t.Fatalf("decodeLegacyValues: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("value %q = %v, want %v", k, got[k], v)
		}
	}
}

// A session written by turna v0.8.x must be readable by the v1 compat mode.
func TestDecodeLegacyValuesFromOldStore(t *testing.T) {
	data := legacyPayload(t, map[any]any{
		"token":    "old-token",
		"provider": "keycloak",
	})

	got, err := decodeLegacyValues(data)
	if err != nil {
		t.Fatalf("decodeLegacyValues: %v", err)
	}

	if got["token"] != "old-token" {
		t.Errorf("token = %v, want old-token", got["token"])
	}
	if got["provider"] != "keycloak" {
		t.Errorf("provider = %v, want keycloak", got["provider"])
	}
}

// The current session type only supports string keys, so a non-string key is
// skipped rather than failing the whole session.
func TestDecodeLegacyValuesSkipsNonStringKeys(t *testing.T) {
	data := legacyPayload(t, map[any]any{
		"token": "keep",
		42:      "drop",
	})

	got, err := decodeLegacyValues(data)
	if err != nil {
		t.Fatalf("decodeLegacyValues: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d values, want 1: %v", len(got), got)
	}

	if got["token"] != "keep" {
		t.Errorf("token = %v, want keep", got["token"])
	}
}

func TestDecodeLegacyValuesRejectsGarbage(t *testing.T) {
	if _, err := decodeLegacyValues([]byte("not-gob")); err == nil {
		t.Fatal("expected an error for a non-gob payload")
	}
}

// Mixed mode has to read both formats.
func TestDecodeAnyValues(t *testing.T) {
	jsonData, err := json.Marshal(map[string]any{"token": "new-token"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := decodeAnyValues(jsonData)
	if err != nil {
		t.Fatalf("decodeAnyValues(json): %v", err)
	}
	if got["token"] != "new-token" {
		t.Errorf("json token = %v, want new-token", got["token"])
	}

	gobData := legacyPayload(t, map[any]any{"token": "old-token"})

	got, err = decodeAnyValues(gobData)
	if err != nil {
		t.Fatalf("decodeAnyValues(gob): %v", err)
	}
	if got["token"] != "old-token" {
		t.Errorf("gob token = %v, want old-token", got["token"])
	}
}

// The JSON/gob fallback is only unambiguous because a gob payload never parses
// as a JSON object.
func TestDecodeJSONValuesRejectsLegacyPayload(t *testing.T) {
	gobData := legacyPayload(t, map[any]any{"token": "old-token"})

	if _, err := decodeJSONValues(gobData); err == nil {
		t.Fatal("expected a gob payload to fail JSON decoding")
	}
}

// compatStore builds a store without a Redis client so the cookie format can
// be exercised on its own.
func compatStore(compat string) *RedisStore {
	return &RedisStore{
		codec:  securecookie.New([]byte("test-session-key"), nil),
		compat: compat,
	}
}

// v1 must put the raw session ID in the cookie and read it back unsigned, which
// is what turna v0.8.x speaks.
func TestCookieFormatV1(t *testing.T) {
	s := compatStore(CompatV1)

	value, err := s.cookieValue("finops_auth", "id-123")
	if err != nil {
		t.Fatalf("cookieValue: %v", err)
	}

	if value != "id-123" {
		t.Fatalf("cookie value = %q, want the raw id", value)
	}

	got, err := s.sessionID("finops_auth", value)
	if err != nil {
		t.Fatalf("sessionID: %v", err)
	}

	if got != "id-123" {
		t.Errorf("sessionID = %q, want id-123", got)
	}
}

func TestCookieFormatDefault(t *testing.T) {
	s := compatStore(CompatNone)

	value, err := s.cookieValue("finops_auth", "id-123")
	if err != nil {
		t.Fatalf("cookieValue: %v", err)
	}

	if value == "id-123" {
		t.Fatal("cookie value should be signed, not the raw id")
	}

	got, err := s.sessionID("finops_auth", value)
	if err != nil {
		t.Fatalf("sessionID: %v", err)
	}

	if got != "id-123" {
		t.Errorf("sessionID = %q, want id-123", got)
	}

	// An unsigned legacy cookie must be rejected outside compat mode.
	if _, err := s.sessionID("finops_auth", "id-123"); err == nil {
		t.Error("expected a legacy cookie to be rejected in the default mode")
	}
}

// Mixed accepts a legacy cookie but issues a signed one, which migrates
// sessions without logging users out.
func TestCookieFormatMixed(t *testing.T) {
	s := compatStore(CompatMixed)

	got, err := s.sessionID("finops_auth", "legacy-id")
	if err != nil {
		t.Fatalf("sessionID(legacy): %v", err)
	}

	if got != "legacy-id" {
		t.Errorf("sessionID(legacy) = %q, want legacy-id", got)
	}

	value, err := s.cookieValue("finops_auth", "legacy-id")
	if err != nil {
		t.Fatalf("cookieValue: %v", err)
	}

	if value == "legacy-id" {
		t.Fatal("mixed mode should issue a signed cookie")
	}

	got, err = s.sessionID("finops_auth", value)
	if err != nil {
		t.Fatalf("sessionID(signed): %v", err)
	}

	if got != "legacy-id" {
		t.Errorf("sessionID(signed) = %q, want legacy-id", got)
	}
}

func TestRedisStoreUnknownCompat(t *testing.T) {
	// Validation runs before the client is dialed, so no Redis is required.
	_, err := Redis{Compat: "bogus"}.Store(context.Background(), sessions.Options{})
	if err == nil {
		t.Fatal("expected an error for an unknown compat mode")
	}

	if !strings.Contains(err.Error(), "compat") {
		t.Errorf("error %q should mention compat", err)
	}
}
