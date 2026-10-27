package webhooksignature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const body = `{"event":"push"}`

func sign(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))

	return mac.Sum(nil)
}

func serve(t *testing.T, m *WebhookSignature, headers map[string]string) (int, string) {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	var got string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code, got
}

func TestGitHub(t *testing.T) {
	m := &WebhookSignature{Preset: "github", Secrets: []string{"old", "new"}}
	sig := "sha256=" + hex.EncodeToString(sign([]byte("new"), body))

	code, got := serve(t, m, map[string]string{"X-Hub-Signature-256": sig})
	if code != http.StatusNoContent || got != body {
		t.Fatalf("status = %d, body %q", code, got)
	}

	for name, h := range map[string]map[string]string{
		"missing":   {},
		"wrong":     {"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(sign([]byte("other"), body))},
		"no prefix": {"X-Hub-Signature-256": hex.EncodeToString(sign([]byte("new"), body))},
	} {
		if code, _ := serve(t, m, h); code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d", name, code)
		}
	}
}

func TestStripe(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := hex.EncodeToString(sign([]byte("whsec"), ts+"."+body))

	m := &WebhookSignature{Preset: "stripe", Secrets: []string{"whsec"}}

	if code, _ := serve(t, m, map[string]string{"Stripe-Signature": "t=" + ts + ",v1=00,v1=" + sig}); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}

	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	oldSig := hex.EncodeToString(sign([]byte("whsec"), old+"."+body))
	if code, _ := serve(t, m, map[string]string{"Stripe-Signature": "t=" + old + ",v1=" + oldSig}); code != http.StatusUnauthorized {
		t.Fatalf("old timestamp status = %d", code)
	}
}

func TestSlack(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "v0=" + hex.EncodeToString(sign([]byte("s"), "v0:"+ts+":"+body))

	m := &WebhookSignature{Preset: "slack", Secrets: []string{"s"}}

	code, _ := serve(t, m, map[string]string{"X-Slack-Signature": sig, "X-Slack-Request-Timestamp": ts})
	if code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
}

func TestShopify(t *testing.T) {
	sig := base64.StdEncoding.EncodeToString(sign([]byte("s"), body))
	m := &WebhookSignature{Preset: "shopify", Secrets: []string{"s"}}

	if code, _ := serve(t, m, map[string]string{"X-Shopify-Hmac-Sha256": sig}); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
}

func TestStandard(t *testing.T) {
	key := []byte("0123456789abcdef")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := base64.StdEncoding.EncodeToString(sign(key, "msg_1."+ts+"."+body))

	m := &WebhookSignature{Preset: "standard", Secrets: []string{"whsec_" + base64.StdEncoding.EncodeToString(key)}}

	code, _ := serve(t, m, map[string]string{
		"Webhook-Id":        "msg_1",
		"Webhook-Timestamp": ts,
		"Webhook-Signature": "v1,AAAA v1," + sig,
	})
	if code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
}

func TestGenericTimestamp(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	m := &WebhookSignature{Header: "X-Signature", TimestampHeader: "X-Timestamp", Secrets: []string{"s"}, StatusCode: 403}

	sig := hex.EncodeToString(sign([]byte("s"), ts+"."+body))
	if code, _ := serve(t, m, map[string]string{"X-Signature": sig, "X-Timestamp": ts}); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}

	if code, _ := serve(t, m, map[string]string{"X-Signature": sig}); code != http.StatusForbidden {
		t.Fatalf("missing timestamp status = %d", code)
	}
}

func TestBodyLimit(t *testing.T) {
	m := &WebhookSignature{Header: "X-Signature", Secrets: []string{"s"}, MaxBodySize: "4B"}

	if code, _ := serve(t, m, map[string]string{"X-Signature": "00"}); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", code)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*WebhookSignature{
		{Preset: "github"},
		{Secrets: []string{"s"}},
		{Preset: "nope", Secrets: []string{"s"}},
		{Header: "X", Secrets: []string{"s"}, Algorithm: "md5"},
		{Header: "X", Secrets: []string{"s"}, Encoding: "b32"},
		{Header: "X", Secrets: []string{"zz"}, SecretEncoding: "hex"},
		{Header: "X", Secrets: []string{""}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
