package waf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	crsOnce    sync.Once
	crsHandler http.Handler
	crsErr     error
	crsInit    time.Duration
)

func ok(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func build(t *testing.T, m *WAF) http.Handler {
	t.Helper()

	mw, err := m.Middleware(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(ok))
}

func crs(t *testing.T) http.Handler {
	t.Helper()

	crsOnce.Do(func() {
		start := time.Now()

		mw, err := (&WAF{CRS: true}).Middleware(context.Background())
		crsInit = time.Since(start)
		if err != nil {
			crsErr = err

			return
		}

		crsHandler = mw(http.HandlerFunc(ok))
	})

	if crsErr != nil {
		t.Fatal(crsErr)
	}

	t.Logf("crs init %s", crsInit)

	return crsHandler
}

func do(h http.Handler, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("User-Agent", "turna-test")
	req.Header.Set("Accept", "*/*")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestCRS(t *testing.T) {
	h := crs(t)

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{"normal", "/hello?name=world", http.StatusOK},
		{"sqli", "/?id=1%27%20OR%20%271%27=%271", http.StatusForbidden},
		{"xss", "/?q=%3Cscript%3Ealert(1)%3C/script%3E", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(h, tt.target)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d, body %q", rec.Code, tt.want, rec.Body.String())
			}

			if tt.want == http.StatusForbidden {
				if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
					t.Fatalf("content type = %q", ct)
				}
			}
		})
	}
}

func TestDetectionOnly(t *testing.T) {
	h := build(t, &WAF{
		Mode:       "detection_only",
		Directives: `SecRule ARGS:foo "@streq bar" "id:1001,phase:1,deny,status:418"`,
	})

	if rec := do(h, "/?foo=bar"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestCustomDirective(t *testing.T) {
	h := build(t, &WAF{
		Directives: `SecRule ARGS:foo "@streq bar" "id:1001,phase:1,deny,status:418"`,
	})

	if rec := do(h, "/?foo=bar"); rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}

	if rec := do(h, "/?foo=baz"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestDirectivesFiles(t *testing.T) {
	dir := t.TempDir()
	rule := `SecRule ARGS:foo "@streq bar" "id:1002,phase:1,deny"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "a.conf"), []byte(rule), 0o600); err != nil {
		t.Fatal(err)
	}

	h := build(t, &WAF{DirectivesFiles: []string{filepath.Join(dir, "*.conf")}})

	if rec := do(h, "/?foo=bar"); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestOff(t *testing.T) {
	h := build(t, &WAF{
		Mode:       "off",
		Directives: `SecRule ARGS:foo "@streq bar" "id:1001,phase:1,deny"`,
	})

	if rec := do(h, "/?foo=bar"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestParanoiaLevel(t *testing.T) {
	m := &WAF{CRS: true, ParanoiaLevel: 2}

	set, err := m.directives("On")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(set.before, "tx.blocking_paranoia_level=2") {
		t.Fatalf("paranoia level not set:\n%s", set.before)
	}

	setup := strings.Index(set.before, "@crs-setup.conf.example")
	pl := strings.Index(set.before, "blocking_paranoia_level")
	rules := strings.Index(set.before, "@owasp_crs/")
	if setup >= pl || pl >= rules {
		t.Fatal("paranoia level must be set between crs-setup and rules")
	}

	if testing.Short() {
		return
	}

	if _, err := m.Middleware(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInvalid(t *testing.T) {
	tests := []struct {
		name string
		m    *WAF
	}{
		{"directive", &WAF{Directives: "SecNotADirective On"}},
		{"rule", &WAF{Directives: `SecRule ARGS "@nope x" "id:1,phase:1,deny"`}},
		{"mode", &WAF{Mode: "maybe"}},
		{"paranoia", &WAF{ParanoiaLevel: 5}},
		{"file", &WAF{DirectivesFiles: []string{"/nonexistent/*.conf"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.m.Middleware(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}

			if !strings.HasPrefix(err.Error(), "waf") {
				t.Fatalf("error %q is not prefixed with waf", err)
			}
		})
	}
}

func TestConfigUnchanged(t *testing.T) {
	m := &WAF{Mode: " Detection_Only ", Directives: `SecRule ARGS:debug "@streq true" "id:1002,phase:1,deny"`}
	before, _ := json.Marshal(m)

	build(t, m)

	if after, _ := json.Marshal(m); string(after) != string(before) {
		t.Fatalf("config changed by Middleware: %s -> %s", before, after)
	}
}
