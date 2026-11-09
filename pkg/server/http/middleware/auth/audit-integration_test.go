package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAuditIntegration checks that sign-ins, token failures and admin
// changes land in the audit log, the API filters them and the webhook is
// signed.
func TestAuditIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		hookM    sync.Mutex
		hookBody []AuditEvent
		hookOK   = true
	)
	const hookSecret = "audit-webhook-secret"
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(hookSecret))
		mac.Write([]byte(r.Header.Get("X-Turna-Timestamp") + "."))
		mac.Write(body)

		hookM.Lock()
		defer hookM.Unlock()
		if r.Header.Get("X-Turna-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			hookOK = false
		}
		var ev AuditEvent
		_ = json.Unmarshal(body, &ev)
		hookBody = append(hookBody, ev)
	}))
	defer hook.Close()

	m := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: "integration-test-key"}}
	middleware, err := m.Middleware(ctx, "auth-audit")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	put := func(path, body string) int {
		req, _ := http.NewRequest(http.MethodPut, server.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User", "audit-admin")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()

		return res.StatusCode
	}

	if code := put("/auth/v1/settings/audit", `{"value":{"webhook_url":"`+hook.URL+`","webhook_secret":"`+hookSecret+`","webhook_events":["login"]}}`); code != http.StatusOK {
		t.Fatalf("put audit setting: %d", code)
	}
	if code := put("/auth/v1/settings/audit", `{"value":{"retention":"nope"}}`); code != http.StatusBadRequest {
		t.Fatalf("invalid retention: %d", code)
	}
	if code := put("/auth/v1/oauth/clients/it-audit-client", `{"config":{"client_secret":"it-audit-secret","scope":["openid"]}}`); code != http.StatusOK {
		t.Fatalf("put client: %d", code)
	}
	t.Cleanup(func() { _, _ = m.store.DeleteOAuthClient(context.Background(), "it-audit-client") })

	{
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/v1/users",
			strings.NewReader(`{"alias":["it-audit-user"],"local":true,"details":{"password":"it-audit-pass-123"},"is_active":true}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var created struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		_ = json.NewDecoder(res.Body).Decode(&created)
		res.Body.Close()
		t.Cleanup(func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/users/"+created.Payload.ID, nil)
			if res, _ := http.DefaultClient.Do(req); res != nil {
				res.Body.Close()
			}
		})
	}

	start := time.Now().Add(-time.Second)

	login := func(password string) int {
		form := url.Values{"grant_type": {"password"}, "username": {"it-audit-user"}, "password": {password}}
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "audit-test")
		req.SetBasicAuth("it-audit-client", "it-audit-secret")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()

		return res.StatusCode
	}

	if code := login("it-audit-pass-123"); code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	if code := login("wrong-password"); code == http.StatusOK {
		t.Fatal("wrong password must fail")
	}

	list := func(query string) []AuditEvent {
		t.Helper()

		var out Response[[]AuditEvent]
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			res, err := http.Get(server.URL + "/auth/v1/audit?since=" + url.QueryEscape(start.Format(time.RFC3339)) + "&" + query)
			if err != nil {
				t.Fatal(err)
			}
			_ = json.NewDecoder(res.Body).Decode(&out)
			res.Body.Close()
			if len(out.Payload) > 0 {
				return out.Payload
			}
			time.Sleep(100 * time.Millisecond)
		}

		return out.Payload
	}

	t.Run("successful login", func(t *testing.T) {
		events := list("event=login&outcome=success")
		if len(events) == 0 {
			t.Fatal("no login success event")
		}
		ev := events[0]
		if ev.Subject == "" || ev.ClientID != "it-audit-client" || ev.UserAgent != "audit-test" || ev.IP == "" {
			t.Fatalf("event = %+v", ev)
		}
		if ev.Detail["grant_type"] != "password" {
			t.Fatalf("detail = %v", ev.Detail)
		}
	})

	t.Run("failed login names the username", func(t *testing.T) {
		events := list("event=login&outcome=failure")
		if len(events) == 0 || events[0].Subject != "it-audit-user" {
			t.Fatalf("events = %+v", events)
		}
		if _, ok := events[0].Detail["password"]; ok {
			t.Fatal("password must never be recorded")
		}
	})

	t.Run("admin changes", func(t *testing.T) {
		events := list("event=admin.change&actor=audit-admin")
		if len(events) < 2 {
			t.Fatalf("admin events = %+v", events)
		}
		var sawFailure bool
		for _, ev := range events {
			if ev.Outcome == auditFailure {
				sawFailure = true
			}
		}
		if !sawFailure {
			t.Fatal("rejected write must be recorded as failure")
		}
	})

	t.Run("webhook is signed and filtered", func(t *testing.T) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			hookM.Lock()
			n := len(hookBody)
			hookM.Unlock()
			if n >= 2 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		hookM.Lock()
		defer hookM.Unlock()
		if !hookOK {
			t.Fatal("webhook signature mismatch")
		}
		if len(hookBody) < 2 {
			t.Fatalf("webhook events = %d", len(hookBody))
		}
		for _, ev := range hookBody {
			if ev.Event != auditLogin {
				t.Fatalf("webhook_events filter leaked %q", ev.Event)
			}
		}
	})

	t.Run("webhook secret is write-only", func(t *testing.T) {
		res, err := http.Get(server.URL + "/auth/v1/settings/audit")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if strings.Contains(string(body), hookSecret) || !strings.Contains(string(body), `"webhook_secret_set":true`) {
			t.Fatalf("setting = %s", body)
		}
	})
}
